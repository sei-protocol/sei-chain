package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type GasLimitDecorator struct {
	evmKeeper *evmkeeper.Keeper
}

func NewGasLimitDecorator(evmKeeper *evmkeeper.Keeper) *GasLimitDecorator {
	return &GasLimitDecorator{evmKeeper: evmKeeper}
}

// Called at the end of the ante chain to set gas limit properly
func (gl GasLimitDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msg := evmtypes.MustGetEVMTransactionMessage(tx)
	txData, err := evmtypes.UnpackTxData(msg.Data)
	if err != nil {
		return ctx, err
	}

	adjustedGasLimit := gl.evmKeeper.GetPriorityNormalizer(ctx).MulInt64(int64(txData.GetGas()))
	ctx = ctx.WithGasMeter(utils.NewGasMeterWithMultiplier(ctx, adjustedGasLimit.TruncateInt().Uint64()))
	return next(ctx, tx, simulate)
}
