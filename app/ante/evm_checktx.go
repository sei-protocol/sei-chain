package ante

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	upgradekeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/keeper"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/helpers"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	evmante "github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

func EvmCheckTxAnte(
	ctx sdk.Context,
	txConfig client.TxConfig,
	tx sdk.Tx,
	upgradeKeeper *upgradekeeper.Keeper,
	ek *evmkeeper.Keeper,
	latestCtxGetter func() sdk.Context,
) (returnCtx sdk.Context, returnErr error) {
	chainID := ek.ChainID(ctx)
	if err := EvmStatelessChecks(ctx, tx, chainID); err != nil {
		return ctx, err
	}
	msg := tx.GetMsgs()[0].(*evmtypes.MsgEVMTransaction)

	txData, _ := evmtypes.UnpackTxData(msg.Data) // cached and validated
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	if atx, ok := txData.(*ethtx.AssociateTx); ok {
		return HandleAssociateTx(ctx, ek, atx, true)
	}
	etx := ethtypes.NewTx(txData.AsEthereumData())
	evmAddr, seiAddr, seiPubkey, version, err := CheckAndDecodeSignature(ctx, txData, chainID, false)
	if err != nil {
		return ctx, err
	}
	if err := AssociateAddress(ctx, ek, evmAddr, seiAddr, seiPubkey); err != nil {
		return ctx, err
	}
	if _, err := EvmCheckAndChargeFees(ctx, evmAddr, ek, upgradeKeeper, txData, etx, msg, version, false); err != nil {
		return ctx, err
	}

	ctx, err = CheckNonce(ctx, latestCtxGetter, ek, etx, evmAddr, seiAddr)
	if err != nil {
		return ctx, err
	}

	return DecorateContext(ctx, ek, tx, txData, etx, evmAddr), nil
}

func EvmStatelessChecks(ctx sdk.Context, tx sdk.Tx, chainID *big.Int) error {
	txBody, ok := tx.(TxBody)
	if ok {
		body := txBody.GetBody()
		if body.Memo != "" {
			return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "memo must be empty for EVM txs")
		}
		if body.TimeoutHeight != 0 {
			return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "timeout_height must be zero for EVM txs")
		}
		if len(body.ExtensionOptions) > 0 || len(body.NonCriticalExtensionOptions) > 0 {
			return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "extension options must be empty for EVM txs")
		}
	}

	txAuth, ok := tx.(TxAuthInfo)
	if ok {
		authInfo := txAuth.GetAuthInfo()
		if len(authInfo.SignerInfos) > 0 {
			return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "signer_infos must be empty for EVM txs")
		}
		if authInfo.Fee != nil {
			if len(authInfo.Fee.Amount) > 0 {
				return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "fee amount must be empty for EVM txs")
			}
			if authInfo.Fee.Payer != "" || authInfo.Fee.Granter != "" {
				return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "fee payer and granter must be empty for EVM txs")
			}
		}
	}

	txSig, ok := tx.(TxSignaturesV2)
	if ok {
		sigs, err := txSig.GetSignaturesV2()
		if err != nil {
			return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "could not get signatures")
		}
		if len(sigs) > 0 {
			return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "signatures must be empty for EVM txs")
		}
	}

	if len(tx.GetMsgs()) != 1 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "EVM transaction must have exactly 1 message")
	}
	msg, ok := tx.GetMsgs()[0].(*evmtypes.MsgEVMTransaction)
	if !ok {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "not EVM message")
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	if msg.Derived != nil && msg.Derived.PubKey == nil {
		// this means the message has `Derived` set from the outside, in which case we should reject
		return sdkerrors.ErrInvalidPubKey
	}
	txData, err := evmtypes.UnpackTxData(msg.Data)
	if err != nil {
		return err
	}
	if _, ok := txData.(*ethtx.AssociateTx); ok {
		return nil
	}
	etx, _ := msg.AsTransaction()
	if etx.To() == nil && len(etx.Data()) > params.MaxInitCodeSize {
		return fmt.Errorf("%w: code size %v, limit %v", core.ErrMaxInitCodeSizeExceeded, len(etx.Data()), params.MaxInitCodeSize)
	}

	if etx.Value().Sign() < 0 {
		return sdkerrors.ErrInvalidCoins
	}

	intrGas, err := core.IntrinsicGas(etx.Data(), etx.AccessList(), etx.SetCodeAuthorizations(), etx.To() == nil, true, true, true)
	if err != nil {
		return err
	}
	if etx.Gas() < intrGas {
		return core.ErrIntrinsicGas
	}

	if etx.Type() == ethtypes.BlobTxType {
		return sdkerrors.ErrUnsupportedTxType
	}

	// Check if gas exceed the limit
	if cp := ctx.ConsensusParams(); cp != nil && cp.Block != nil {
		// If there exists a maximum block gas limit, we must ensure that the tx
		// does not exceed it.
		if cp.Block.MaxGas > 0 && etx.Gas() > uint64(cp.Block.MaxGas) { //nolint:gosec
			return sdkerrors.Wrapf(sdkerrors.ErrOutOfGas, "tx gas limit %d exceeds block max gas %d", etx.Gas(), cp.Block.MaxGas)
		}
	}

	if txData.GetGasTipCap().Sign() < 0 {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "gas fee cap cannot be negative")
	}

	// validate chain ID on the transaction
	txChainID := etx.ChainId()
	switch etx.Type() {
	case ethtypes.LegacyTxType:
		// legacy either can have a zero or correct chain ID
		if txChainID.Cmp(big.NewInt(0)) != 0 && txChainID.Cmp(chainID) != 0 {
			ctx.Logger().Debug("chainID mismatch", "txChainID", txChainID, "chainID", chainID)
			return sdkerrors.ErrInvalidChainID
		}
	default:
		// after legacy, all transactions must have the correct chain ID
		if txChainID.Cmp(chainID) != 0 {
			ctx.Logger().Debug("chainID mismatch", "txChainID", txChainID, "chainID", chainID)
			return sdkerrors.ErrInvalidChainID
		}
	}

	txGas := txData.GetGas()
	if txGas > math.MaxInt64 {
		return errors.New("tx gas exceeds max")
	}
	return nil
}

func DecorateContext(ctx sdk.Context, ek *evmkeeper.Keeper, tx sdk.Tx, txData ethtx.TxData, etx *ethtypes.Transaction, sender common.Address) sdk.Context {
	ctx = ctx.WithPriority(CalculatePriority(ctx, txData, ek).Int64())

	// set EVM properties
	ctx = ctx.WithIsEVM(true)
	ctx = ctx.WithEVMNonce(etx.Nonce())
	ctx = ctx.WithEVMSenderAddress(sender.Hex())
	ctx = ctx.WithEVMTxHash(etx.Hash().Hex())
	adjustedGasLimit := ek.GetPriorityNormalizer(ctx).MulInt64(int64(txData.GetGas())) //nolint:gosec
	gasMeter := sdk.NewGasMeterWithMultiplier(ctx, adjustedGasLimit.TruncateInt().Uint64())
	ctx = ctx.WithGasMeter(gasMeter)
	if tx.GetGasEstimate() >= evmante.MinGasEVMTx {
		ctx = ctx.WithGasEstimate(tx.GetGasEstimate())
	} else {
		ctx = ctx.WithGasEstimate(gasMeter.Limit())
	}
	return ctx
}

func HandleAssociateTx(ctx sdk.Context, ek *evmkeeper.Keeper, atx *ethtx.AssociateTx, readOnly bool) (sdk.Context, error) {
	V, R, S := atx.GetRawSignatureValues()
	V = new(big.Int).Add(V, utils.Big27)
	// Hash custom message passed in
	customMessageHash := crypto.Keccak256Hash([]byte(atx.CustomMessage))
	evmAddr, seiAddr, seiPubkey, err := helpers.GetAddresses(V, R, S, customMessageHash)
	if err != nil {
		return ctx, err
	}
	_, isAssociated := ek.GetEVMAddress(ctx, seiAddr)
	if isAssociated {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "account already has association set")
	}
	if !IsAccountBalancePositive(ctx, ek, seiAddr, evmAddr) {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInsufficientFunds, "account needs to have at least 1 wei to force association")
	}
	if !readOnly {
		if err := AssociateAddress(ctx, ek, evmAddr, seiAddr, seiPubkey); err != nil {
			return ctx, err
		}
	}
	return ctx.WithPriority(antedecorators.EVMAssociatePriority), nil
}

func CheckAndDecodeSignature(ctx sdk.Context, txData ethtx.TxData, chainID *big.Int, isBlockTest bool) (common.Address, sdk.AccAddress, cryptotypes.PubKey, derived.SignerVersion, error) {
	ethTx := ethtypes.NewTx(txData.AsEthereumData())
	if ethTx.Type() != ethtypes.LegacyTxType {
		chainID = ethTx.ChainId()
	}
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	version := evmante.GetVersion(ctx, ethCfg)
	signer := evmante.SignerMap[version](chainID)
	if !evmante.IsTxTypeAllowed(version, ethTx.Type()) {
		return common.Address{}, sdk.AccAddress{}, nil, 0, ethtypes.ErrInvalidChainId
	}

	var txHash common.Hash
	V, R, S := ethTx.RawSignatureValues()
	if ethTx.Protected() {
		V = helpers.AdjustV(V, ethTx.Type(), ethCfg.ChainID)
		txHash = signer.Hash(ethTx)
	} else {
		if isBlockTest {
			// need to allow unprotected legacy txs in blocktest
			// to not lose coverage for other parts of the code
			txHash = ethtypes.FrontierSigner{}.Hash(ethTx)
		} else {
			return common.Address{}, sdk.AccAddress{}, nil, 0, errors.New("unsupported tx type: unsafe legacy tx")
		}
	}
	evmAddr, seiAddr, seiPubkey, err := helpers.GetAddresses(V, R, S, txHash)
	if err != nil {
		return common.Address{}, sdk.AccAddress{}, nil, 0, sdkerrors.ErrInvalidChainID
	}
	return evmAddr, seiAddr, seiPubkey, version, nil
}

func AssociateAddress(ctx sdk.Context, ek *evmkeeper.Keeper, evmAddr common.Address, seiAddr sdk.AccAddress, seiPubkey cryptotypes.PubKey) error {
	_, isAssociated := ek.GetEVMAddress(ctx, seiAddr)
	if !isAssociated {
		associateHelper := helpers.NewAssociationHelper(ek, ek.BankKeeper(), ek.AccountKeeper())
		if err := associateHelper.AssociateAddresses(ctx, seiAddr, evmAddr, seiPubkey, false); err != nil {
			return err
		}
	}
	return nil
}

func EvmCheckAndChargeFees(ctx sdk.Context, sender common.Address, ek *evmkeeper.Keeper, upgradeKeeper *upgradekeeper.Keeper, txData ethtx.TxData, etx *ethtypes.Transaction, msg *evmtypes.MsgEVMTransaction, version derived.SignerVersion, statelessChecks bool) (*state.DBImpl, error) {
	if txData.GetGasFeeCap().Cmp(GetBaseFee(ctx, ek, upgradeKeeper)) < 0 {
		return nil, sdkerrors.ErrInsufficientFee
	}
	if txData.GetGasFeeCap().Cmp(GetMinimumFee(ctx, ek)) < 0 {
		return nil, sdkerrors.ErrInsufficientFee
	}
	if version >= derived.Cancun && len(txData.GetBlobHashes()) > 0 {
		// For now we are simply assuming excessive blob gas is 0. In the future we might change it to be
		// dynamic based on prior block usage.
		chainConfig := evmtypes.DefaultChainConfig().EthereumConfig(ek.ChainID(ctx))
		if txData.GetBlobFeeCap().Cmp(eip4844.CalcBlobFee(chainConfig, &ethtypes.Header{Time: uint64(ctx.BlockTime().Unix())})) < 0 { // nolint:gosec
			return nil, sdkerrors.ErrInsufficientFee
		}
	}
	emsg := ek.GetEVMMessage(ctx, etx, sender)
	stateDB := state.NewDBImpl(ctx, ek, false)
	gp := ek.GetGasPool()
	blockCtx, err := ek.GetVMBlockContext(ctx, gp)
	if err != nil {
		return nil, err
	}
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(ek.ChainID(ctx))
	txCtx := core.NewEVMTxContext(emsg)
	evmInstance := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, ek.CustomPrecompiles(ctx))
	evmInstance.SetTxContext(txCtx)
	st := core.NewStateTransition(evmInstance, emsg, &gp, true, false)
	if statelessChecks {
		if err := st.StatelessChecks(); err != nil {
			return nil, sdkerrors.Wrap(sdkerrors.ErrWrongSequence, err.Error())
		}
	}
	if err := st.BuyGas(); err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInsufficientFunds, err.Error())
	}
	return stateDB, nil
}

func CheckNonce(ctx sdk.Context, latestCtxGetter func() sdk.Context, ek *evmkeeper.Keeper, etx *ethtypes.Transaction, evmAddr common.Address, seiAddr sdk.AccAddress) (sdk.Context, error) {
	fee := new(big.Int).Mul(etx.GasPrice(), new(big.Int).SetUint64(etx.Gas()))
	if etx.Value() != nil {
		fee = new(big.Int).Add(fee, etx.Value())
	}

	txNonce := etx.Nonce()
	nextNonce := ek.GetNonce(ctx, evmAddr)
	if txNonce < nextNonce {
		return ctx, sdkerrors.ErrWrongSequence
	}
	ctx = ctx.WithCheckTxCallback(func(priority int64) {
		txKey := tmtypes.Tx(ctx.TxBytes()).Key()
		ek.AddPendingNonce(txKey, evmAddr, etx.Nonce(), priority)
		metrics.IncrementPendingNonce("added")
	})

	// if the mempool expires a transaction, this handler is invoked
	ctx = ctx.WithExpireTxHandler(func() {
		txKey := tmtypes.Tx(ctx.TxBytes()).Key()
		ek.RemovePendingNonce(txKey)
		metrics.IncrementPendingNonce("expired")
	})

	if txNonce > nextNonce {
		// transaction shall be added to mempool as a pending transaction
		ctx = ctx.WithPendingTxChecker(func() abci.PendingTxCheckerResponse {
			latestCtx := latestCtxGetter()

			// nextNonceToBeMined is the next nonce that will be mined
			// geth calls SetNonce(n+1) after a transaction is mined
			nextNonceToBeMined := ek.GetNonce(latestCtx, evmAddr)

			// nextPendingNonce is the minimum nonce a user may send without stomping on an already-sent
			// nonce, including non-mined or pending transactions
			// If a user skips a nonce [1,2,4], then this will be the value of that hole (e.g., 3)
			nextPendingNonce := ek.CalculateNextNonce(latestCtx, evmAddr, true)

			if txNonce < nextNonceToBeMined {
				// this nonce has already been mined, we cannot accept it again
				metrics.IncrementPendingNonce("rejected")
				return abci.Rejected
			} else if txNonce < nextPendingNonce {
				// check if the sender still has enough funds to pay for gas
				balance := ek.GetBalance(latestCtx, seiAddr)
				if balance.Cmp(fee) < 0 {
					// not enough funds. Go back to pending as it may obtain sufficient funds later.
					return abci.Pending
				}
				// this nonce is allowed to process as it is part of the
				// consecutive nonces from nextNonceToBeMined to nextPendingNonce
				// This logic allows multiple nonces from an account to be processed in a block.
				metrics.IncrementPendingNonce("accepted")
				return abci.Accepted
			}
			return abci.Pending
		})
	}
	return ctx, nil
}

func IsAccountBalancePositive(ctx sdk.Context, evmKeeper *evmkeeper.Keeper, seiAddr sdk.AccAddress, evmAddr common.Address) bool {
	baseDenom := evmKeeper.GetBaseDenom(ctx)
	if amt := evmKeeper.BankKeeper().GetBalance(ctx, seiAddr, baseDenom).Amount; amt.IsPositive() {
		return true
	}
	if amt := evmKeeper.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), baseDenom).Amount; amt.IsPositive() {
		return true
	}
	if amt := evmKeeper.BankKeeper().GetWeiBalance(ctx, seiAddr); amt.IsPositive() {
		return true
	}
	return evmKeeper.BankKeeper().GetWeiBalance(ctx, sdk.AccAddress(evmAddr[:])).IsPositive()
}

// minimum fee per gas required for a tx to be processed
func GetBaseFee(ctx sdk.Context, evmKeeper *evmkeeper.Keeper, upgradeKeeper *upgradekeeper.Keeper) *big.Int {
	if ctx.ChainID() == "pacific-1" && ctx.BlockHeight() < 114945913 {
		return evmKeeper.GetBaseFeePerGas(ctx).TruncateInt().BigInt()
	}
	if ctx.ChainID() == "pacific-1" && ctx.BlockHeight() < upgradeKeeper.GetDoneHeight(ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)), "6.2.0") {
		return evmKeeper.GetCurrBaseFeePerGas(ctx).TruncateInt().BigInt()
	}
	return evmKeeper.GetNextBaseFeePerGas(ctx).TruncateInt().BigInt()
}

// lowest allowed fee per gas, base fee will not be lower than this
func GetMinimumFee(ctx sdk.Context, evmKeeper *evmkeeper.Keeper) *big.Int {
	return evmKeeper.GetMinimumFeePerGas(ctx).TruncateInt().BigInt()
}

func CalculatePriority(ctx sdk.Context, txData ethtx.TxData, evmKeeper *evmkeeper.Keeper) *big.Int {
	gp := txData.EffectiveGasPrice(utils.Big0)
	priority := sdk.NewDecFromBigInt(gp).Quo(evmKeeper.GetPriorityNormalizer(ctx)).TruncateInt().BigInt()
	if priority.Cmp(big.NewInt(antedecorators.MaxPriority)) > 0 {
		priority = big.NewInt(antedecorators.MaxPriority)
	}
	return priority
}
