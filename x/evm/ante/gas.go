package ante

import (
	"errors"
	"math"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	MinGasEVMTx = 21000
)

type GasDecorator struct {
	evmKeeper *evmkeeper.Keeper
}

func NewGasDecorator(evmKeeper *evmkeeper.Keeper) *GasDecorator {
	return &GasDecorator{evmKeeper: evmKeeper}
}

// Called at the end of the ante chain to set gas limit and gas used estimate properly
func (gl GasDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msg := evmtypes.MustGetEVMTransactionMessage(tx)
	txData, err := evmtypes.UnpackTxData(msg.Data)
	if err != nil {
		return ctx, err
	}

	txGas := txData.GetGas()
	if txGas > math.MaxInt64 {
		return ctx, errors.New("tx gas exceeds max")
	}
	adjustedGasLimit := gl.evmKeeper.GetPriorityNormalizer(ctx).MulInt64(int64(txGas)) //nolint:gosec
	gasMeter := sdk.NewGasMeterWithMultiplier(ctx, adjustedGasLimit.TruncateInt().Uint64())
	ctx = ctx.WithGasMeter(gasMeter)
	if tx.GetGasEstimate() >= MinGasEVMTx {
		ctx = ctx.WithGasEstimate(tx.GetGasEstimate())
	} else {
		ctx = ctx.WithGasEstimate(gasMeter.Limit())
	}
	return next(ctx, tx, simulate)
}
