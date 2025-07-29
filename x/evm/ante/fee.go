package ante

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

type EVMFeeCheckDecorator struct {
	evmKeeper     *evmkeeper.Keeper
	upgradeKeeper *upgradekeeper.Keeper
}

func NewEVMFeeCheckDecorator(evmKeeper *evmkeeper.Keeper, upgradeKeeper *upgradekeeper.Keeper) *EVMFeeCheckDecorator {
	return &EVMFeeCheckDecorator{
		evmKeeper:     evmKeeper,
		upgradeKeeper: upgradeKeeper,
	}
}

func (fc EVMFeeCheckDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if simulate {
		return next(ctx, tx, simulate)
	}

	msg := evmtypes.MustGetEVMTransactionMessage(tx)
	txData, err := evmtypes.UnpackTxData(msg.Data)
	if err != nil {
		return ctx, err
	}

	ver := msg.Derived.Version

	if txData.GetGasFeeCap().Cmp(fc.getBaseFee(ctx)) < 0 {
		return ctx, sdkerrors.ErrInsufficientFee
	}
	if txData.GetGasFeeCap().Cmp(fc.getMinimumFee(ctx)) < 0 {
		return ctx, sdkerrors.ErrInsufficientFee
	}
	if txData.GetGasTipCap().Sign() < 0 {
		return ctx, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "gas fee cap cannot be negative")
	}

	// if EVM version is Cancun or later, and the transaction contains at least one blob, we need to
	// make sure the transaction carries a non-zero blob fee cap.
	if ver >= derived.Cancun && len(txData.GetBlobHashes()) > 0 {
		// For now we are simply assuming excessive blob gas is 0. In the future we might change it to be
		// dynamic based on prior block usage.
		chainConfig := evmtypes.DefaultChainConfig().EthereumConfig(fc.evmKeeper.ChainID(ctx))
		if txData.GetBlobFeeCap().Cmp(eip4844.CalcBlobFee(chainConfig, &ethtypes.Header{Time: uint64(ctx.BlockTime().Unix())})) < 0 {
			return ctx, sdkerrors.ErrInsufficientFee
		}
	}

	// check if the sender has enough balance to cover fees
	etx, _ := msg.AsTransaction()
	emsg := fc.evmKeeper.GetEVMMessage(ctx, etx, msg.Derived.SenderEVMAddr)
	stateDB := state.NewDBImpl(ctx, fc.evmKeeper, false)
	gp := fc.evmKeeper.GetGasPool(ctx)
	blockCtx, err := fc.evmKeeper.GetVMBlockContext(ctx, gp)
	if err != nil {
		return ctx, err
	}
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(fc.evmKeeper.ChainID(ctx))
	txCtx := core.NewEVMTxContext(emsg)
	evmInstance := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, fc.evmKeeper.CustomPrecompiles(ctx))
	evmInstance.SetTxContext(txCtx)
	st := core.NewStateTransition(evmInstance, emsg, &gp, true, false)
	// run stateless checks before charging gas (mimicking Geth behavior)
	if !ctx.IsCheckTx() && !ctx.IsReCheckTx() {
		// we don't want to run nonce check here for CheckTx because we have special
		// logic for pending nonce during CheckTx in sig.go
		if err := st.StatelessChecks(); err != nil {
			return ctx, sdkerrors.Wrap(sdkerrors.ErrWrongSequence, err.Error())
		}
	}
	if err := st.BuyGas(); err != nil {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInsufficientFunds, err.Error())
	}
	if !ctx.IsCheckTx() && !ctx.IsReCheckTx() {
		surplus, err := stateDB.Finalize()
		if err != nil {
			return ctx, err
		}
		if err := fc.evmKeeper.AddAnteSurplus(ctx, etx.Hash(), surplus); err != nil {
			return ctx, err
		}
	}

	// calculate the priority by dividing the total fee with the native gas limit (i.e. the effective native gas price)
	priority := fc.CalculatePriority(ctx, txData)
	ctx = ctx.WithPriority(priority.Int64())

	return next(ctx, tx, simulate)
}

// minimum fee per gas required for a tx to be processed
func (fc EVMFeeCheckDecorator) getBaseFee(ctx sdk.Context) *big.Int {
	if ctx.ChainID() == "pacific-1" && ctx.BlockHeight() < 114945913 {
		return fc.evmKeeper.GetBaseFeePerGas(ctx).TruncateInt().BigInt()
	}
	if ctx.ChainID() == "pacific-1" && ctx.BlockHeight() < fc.upgradeKeeper.GetDoneHeight(ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)), "6.2.0") {
		return fc.evmKeeper.GetCurrBaseFeePerGas(ctx).TruncateInt().BigInt()
	}
	return fc.evmKeeper.GetNextBaseFeePerGas(ctx).TruncateInt().BigInt()
}

// lowest allowed fee per gas, base fee will not be lower than this
func (fc EVMFeeCheckDecorator) getMinimumFee(ctx sdk.Context) *big.Int {
	return fc.evmKeeper.GetMinimumFeePerGas(ctx).TruncateInt().BigInt()
}

// CalculatePriority returns a priority based on the effective gas price of the transaction
func (fc EVMFeeCheckDecorator) CalculatePriority(ctx sdk.Context, txData ethtx.TxData) *big.Int {
	gp := txData.EffectiveGasPrice(utils.Big0)
	if !ctx.IsCheckTx() && !ctx.IsReCheckTx() {
		metrics.HistogramEvmEffectiveGasPrice(gp)
	}
	priority := sdk.NewDecFromBigInt(gp).Quo(fc.evmKeeper.GetPriorityNormalizer(ctx)).TruncateInt().BigInt()
	if priority.Cmp(big.NewInt(antedecorators.MaxPriority)) > 0 {
		priority = big.NewInt(antedecorators.MaxPriority)
	}
	return priority
}
