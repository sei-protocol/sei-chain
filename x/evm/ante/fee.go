package ante

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type EVMFeeCheckDecorator struct {
	evmKeeper *evmkeeper.Keeper
}

func NewEVMFeeCheckDecorator(evmKeeper *evmkeeper.Keeper) *EVMFeeCheckDecorator {
	return &EVMFeeCheckDecorator{
		evmKeeper: evmKeeper,
	}
}

func (fc EVMFeeCheckDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// only check fee in CheckTx (similar to normal Sei tx)
	if !ctx.IsCheckTx() || simulate {
		return ctx, nil
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

	// if EVM version is Cancun or later, and the transaction contains at least one blob, we need to
	// make sure the transaction carries a non-zero blob fee cap.
	if ver >= evmtypes.SignerVersion_CANCUN && len(txData.GetBlobHashes()) > 0 {
		// For now we are simply assuming excessive blob gas is 0. In the future we might change it to be
		// dynamic based on prior block usage.
		if txData.GetBlobFeeCap().Cmp(eip4844.CalcBlobFee(0)) < 0 {
			return ctx, sdkerrors.ErrInsufficientFee
		}
	}

	anteCharge := txData.Fee() // this would include blob fee if it's a blob tx

	senderEVMAddr := common.BytesToAddress(evmtypes.MustGetEVMTransactionMessage(tx).Derived.SenderEVMAddr)
	// check if the sender has enough balance to cover fees
	if state.NewDBImpl(ctx, fc.evmKeeper, true).GetBalance(senderEVMAddr).Cmp(anteCharge) < 0 {
		return ctx, sdkerrors.ErrInsufficientFunds
	}

	// calculate the priority by dividing the total fee with the native gas limit (i.e. the effective native gas price)
	priority := new(big.Int).Quo(new(big.Int).Quo(anteCharge, state.UseiToSweiMultiplier), new(big.Int).SetUint64(txData.GetGas()))
	priority = new(big.Int).Quo(priority, fc.evmKeeper.GetPriorityNormalizer(ctx).RoundInt().BigInt())
	ctx = ctx.WithPriority(priority.Int64())

	return next(ctx, tx, simulate)
}

// fee per gas to be burnt
func (fc EVMFeeCheckDecorator) getBaseFee(ctx sdk.Context) *big.Int {
	return fc.evmKeeper.GetBaseFeePerGas(ctx).RoundInt().BigInt()
}

// lowest allowed fee per gas
func (fc EVMFeeCheckDecorator) getMinimumFee(ctx sdk.Context) *big.Int {
	return fc.evmKeeper.GetMinimumFeePerGas(ctx).RoundInt().BigInt()
}
