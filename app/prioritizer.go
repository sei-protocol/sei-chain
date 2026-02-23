package app

import (
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	cosmosante "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/ante"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	upgradekeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/keeper"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/utils"
	evmante "github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

var _ sdk.TxPrioritizer = (*SeiTxPrioritizer)(nil).GetTxPriorityHint

type SeiTxPrioritizer struct {
	evmKeeper     *evmkeeper.Keeper
	upgradeKeeper *upgradekeeper.Keeper
	paramsKeeper  *paramskeeper.Keeper
	logger        log.Logger
}

func NewSeiTxPrioritizer(logger log.Logger, ek *evmkeeper.Keeper, uk *upgradekeeper.Keeper, pk *paramskeeper.Keeper) *SeiTxPrioritizer {
	return &SeiTxPrioritizer{
		logger:        logger,
		evmKeeper:     ek,
		upgradeKeeper: uk,
		paramsKeeper:  pk,
	}
}

func (s *SeiTxPrioritizer) GetTxPriorityHint(ctx sdk.Context, tx sdk.Tx) (_priorityHint int64, _err error) {
	defer func() {
		if r := recover(); r != nil {
			// Fall back to no-op priority if we panic for any reason. This is to avoid DoS
			// vectors where a malicious actor crafts a transaction that panics the
			// prioritizer. Since the prioritizer is used as a hint only, it's safe to fall
			// back to zero priority in this case and log the panic for monitoring purposes.
			s.logger.Error("tx prioritizer panicked. Falling back on no priority", "error", r)
			_priorityHint = 0
			_err = nil
		}
	}()
	if ctx.HasPriority() {
		// The context already has a priority set, return it.
		return ctx.Priority(), nil
	}

	if ok, err := evmante.IsEVMMessage(tx); err != nil {
		return 0, err
	} else if ok {
		evmTx := evmtypes.GetEVMTransactionMessage(tx)
		if evmTx != nil {
			return s.getEvmTxPriority(ctx, evmTx)
		}
		// This should never happen since IsEVMMessage returned true. But we defensively
		// return zero priority to be safe.
		return 0, nil
	}
	if feeTx, ok := tx.(sdk.FeeTx); ok {
		return s.getCosmosTxPriority(ctx, feeTx)
	}
	return 0, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "Tx must either be EVM or Fee")
}

func (s *SeiTxPrioritizer) getEvmTxPriority(ctx sdk.Context, evmTx *evmtypes.MsgEVMTransaction) (int64, error) {

	// Unpack the transaction data first to avoid double unpacking as part of preprocessing.
	txData, err := evmtypes.UnpackTxData(evmTx.Data)
	if err != nil {
		return 0, err
	}

	if err := evmante.PreprocessUnpacked(ctx, evmTx, s.evmKeeper.ChainID(ctx), s.evmKeeper.EthBlockTestConfig.Enabled, txData); err != nil {
		return 0, err
	}
	if evmTx.Derived.IsAssociate {
		_, isAssociated := s.evmKeeper.GetEVMAddress(
			ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)),
			evmTx.Derived.SenderSeiAddr)
		if !isAssociated {
			// Unassociated associate transactions have the second-highest priority.
			// This is to ensure that associate transactions are processed before
			// regular transactions, but after oracle transactions.
			//
			// Note that we are not checking if sufficient funds are present here to keep the
			// priority calculation fast. CheckTx should fully check the transaction.
			return antedecorators.EVMAssociatePriority, nil
		}
		return 0, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "account already has association set")
	}

	// Check txData for sanity.
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
	if evmTx.Derived != nil && evmTx.Derived.Version >= derived.Cancun && len(txData.GetBlobHashes()) > 0 {
		// For now we are simply assuming excessive blob gas is 0. In the future we might change it to be
		// dynamic based on prior block usage.
		chainConfig := evmtypes.DefaultChainConfig().EthereumConfig(s.evmKeeper.ChainID(ctx))
		if txData.GetBlobFeeCap().Cmp(eip4844.CalcBlobFee(chainConfig, &ethtypes.Header{Time: uint64(ctx.BlockTime().Unix())})) < 0 { //nolint:gosec
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
	var igas int64
	if gas > math.MaxInt64 {
		igas = math.MaxInt64
	} else {
		igas = int64(gas) //nolint:gosec
	}

	feeParams := s.paramsKeeper.GetFeesParams(ctx)
	allowedDenoms := feeParams.GetAllowedFeeDenoms()
	denoms := make([]string, 0, len(allowedDenoms)+1)
	denoms = append(denoms, sdk.DefaultBondDenom)
	denoms = append(denoms, allowedDenoms...)
	feeCoins := feeTx.GetFee().NonZeroAmountsOf(denoms)
	priority := cosmosante.GetTxPriority(feeCoins, igas)
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
