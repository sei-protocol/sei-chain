package ante

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
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
	txData, found := evmtypes.GetContextTxData(ctx)
	if !found {
		return ctx, errors.New("could not find eth tx")
	}

	adjustedGasLimit := gl.evmKeeper.GetPriorityNormalizer(ctx).MulInt64(int64(txData.GetGas()))
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(adjustedGasLimit.RoundInt().Uint64()))
	return next(ctx, tx, simulate)
}
