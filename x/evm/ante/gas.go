package ante

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type GasDecorator struct {
	evmKeeper *evmkeeper.Keeper
}

func NewGasDecorator(evmKeeper *evmkeeper.Keeper) *GasDecorator {
	return &GasDecorator{evmKeeper: evmKeeper}
}

// Called at the end of the ante chain to set gas limit and gas used estimate properly
func (gl GasDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	fmt.Println("[DEBUG]: In x/evm/ante/gas.go/AnteHandle: tx.GetGasEstimate() = ", tx.GetGasEstimate())
	msg := evmtypes.MustGetEVMTransactionMessage(tx)
	txData, err := evmtypes.UnpackTxData(msg.Data)
	if err != nil {
		return ctx, err
	}

	adjustedGasLimit := gl.evmKeeper.GetPriorityNormalizer(ctx).MulInt64(int64(txData.GetGas()))
	gasMeter := sdk.NewGasMeterWithMultiplier(ctx, adjustedGasLimit.TruncateInt().Uint64())
	fmt.Printf("[DEBUG]: In x/evm/ante/gas.go/NewGasLimitDecorator: gasMeter limit = %+v\n", gasMeter.Limit())
	ctx = ctx.WithGasMeter(gasMeter)
	if tx.GetGasEstimate() >= 21000 {
		ctx = ctx.WithGasEstimate(tx.GetGasEstimate())
	} else {
		ctx = ctx.WithGasEstimate(gasMeter.GasConsumed())
	}
	return next(ctx, tx, simulate)
}
