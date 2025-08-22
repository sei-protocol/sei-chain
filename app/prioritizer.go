package app

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	cosmosante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/utils"
	evmante "github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

var _ sdk.TxPrioritizer = (*SeiTxPrioritizer)(nil).GetTxPriority

type SeiTxPrioritizer struct {
	evmKeeper     *evmkeeper.Keeper
	upgradeKeeper *upgradekeeper.Keeper
	paramsKeeper  *paramskeeper.Keeper
}

func NewSeiTxPrioritizer(ek *evmkeeper.Keeper, uk *upgradekeeper.Keeper, pk *paramskeeper.Keeper) *SeiTxPrioritizer {
	return &SeiTxPrioritizer{
		evmKeeper:     ek,
		upgradeKeeper: uk,
		paramsKeeper:  pk,
	}
}

func (s *SeiTxPrioritizer) GetTxPriority(ctx sdk.Context, tx sdk.Tx) (int64, error) {
	if ctx.HasPriority() {
		// The context already has a priority set, return it.
		return ctx.Priority(), nil
	}
	if evmTx := evmtypes.GetEVMTransactionMessage(tx); evmTx != nil {
		return s.getEvmTxPriority(ctx, evmTx)
	}
	if feeTx, ok := tx.(sdk.FeeTx); ok {
		return s.getCosmosTxPriority(ctx, feeTx)
	}
	return 0, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "Tx must either be EVM or Fee")
}

func (s *SeiTxPrioritizer) getEvmTxPriority(ctx sdk.Context, evmTx *evmtypes.MsgEVMTransaction) (int64, error) {

	if s.isUnassociatedAssociate(ctx, evmTx) {
		return antedecorators.EVMAssociatePriority, nil
	}

	// Check txData for sanity.
	txData, err := evmtypes.UnpackTxData(evmTx.Data)
	if err != nil {
		return 0, err
	}
	feeCap := txData.GetGasFeeCap()
	fee := s.getEvmBaseFee(ctx)
	if feeCap.Cmp(fee) < 0 {
		return 0, sdkerrors.ErrInsufficientFee
	}
	minimumFee := s.evmKeeper.GetMinimumFeePerGas(ctx).TruncateInt().BigInt()
	if feeCap.Cmp(minimumFee) < 0 {
		return 0, sdkerrors.ErrInsufficientFee
	}
	if txData.GetGasTipCap().Sign() < 0 {
		return 0, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "gas fee cap cannot be negative")
	}
	// Check blob hashes for sanity. If EVM version is Cancun or later, and the
	// transaction contains at least one blob, we need to make sure the transaction
	// carries a non-zero blob fee cap.
	//
	// Note that evmante.Preprocess is a stateless function and would return fast if
	// tx is already pre-processed.
	if err := evmante.Preprocess(ctx, evmTx, s.evmKeeper.ChainID(ctx), s.evmKeeper.EthBlockTestConfig.Enabled); err != nil {
		return 0, err
	}
	if evmTx.Derived != nil && evmTx.Derived.Version >= derived.Cancun && len(txData.GetBlobHashes()) > 0 {
		// For now we are simply assuming excessive blob gas is 0. In the future we might change it to be
		// dynamic based on prior block usage.
		chainConfig := evmtypes.DefaultChainConfig().EthereumConfig(s.evmKeeper.ChainID(ctx))
		if txData.GetBlobFeeCap().Cmp(eip4844.CalcBlobFee(chainConfig, &ethtypes.Header{Time: uint64(ctx.BlockTime().Unix())})) < 0 {
			return 0, sdkerrors.ErrInsufficientFee
		}
	}

	gp := txData.EffectiveGasPrice(utils.Big0)
	priority := sdk.NewDecFromBigInt(gp).Quo(s.evmKeeper.GetPriorityNormalizer(ctx)).TruncateInt().BigInt()
	if priority.Cmp(big.NewInt(antedecorators.MaxPriority)) > 0 {
		priority = big.NewInt(antedecorators.MaxPriority)
	}
	return priority.Int64(), nil
}

func (s *SeiTxPrioritizer) isUnassociatedAssociate(ctx sdk.Context, evmTx *evmtypes.MsgEVMTransaction) bool {
	// TODO: when is derived populated? Check that it is reasonable to use it here.
	if evmTx.Derived == nil {
		return false
	}

	// TODO: this potentially looks up entries from KVstore. Do we want to?
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	_, isAssociated := s.evmKeeper.GetEVMAddress(ctx, evmTx.Derived.SenderSeiAddr)
	return evmTx.Derived.IsAssociate && !isAssociated
}

func (s *SeiTxPrioritizer) getEvmBaseFee(ctx sdk.Context) *big.Int {
	const (
		pacific1              = "pacific-1"
		historicalBlockHeight = 114945913
		doneHeightName        = "6.2.0"
	)
	if ctx.ChainID() == pacific1 {
		height := ctx.BlockHeight()
		if height < historicalBlockHeight {
			return s.evmKeeper.GetBaseFeePerGas(ctx).TruncateInt().BigInt()
		}

		doneHeight := s.upgradeKeeper.GetDoneHeight(
			ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)), doneHeightName)
		if height < doneHeight {
			return s.evmKeeper.GetCurrBaseFeePerGas(ctx).TruncateInt().BigInt()
		}
	}
	return s.evmKeeper.GetNextBaseFeePerGas(ctx).TruncateInt().BigInt()
}

func (s *SeiTxPrioritizer) getCosmosTxPriority(ctx sdk.Context, feeTx sdk.FeeTx) (int64, error) {
	if isOracleTx(feeTx) {
		return antedecorators.OraclePriority, nil
	}

	gas := feeTx.GetGas()
	if gas <= 0 {
		return 0, nil
	}

	feeParams := s.paramsKeeper.GetFeesParams(ctx)
	allowedDenoms := feeParams.GetAllowedFeeDenoms()
	demons := make([]string, 0, len(allowedDenoms)+1)
	demons = append(demons, sdk.DefaultBondDenom)
	demons = append(demons, allowedDenoms...)
	feeCoins := feeTx.GetFee().NonZeroAmountsOf(demons)
	priority := cosmosante.GetTxPriority(feeCoins, int64(gas))
	return min(antedecorators.MaxPriority, priority), nil
}

func isOracleTx(tx sdk.FeeTx) bool {
	if len(tx.GetMsgs()) == 0 {
		return false
	}
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *oracletypes.MsgAggregateExchangeRateVote:
			continue
		default:
			return false
		}
	}
	return true
}
