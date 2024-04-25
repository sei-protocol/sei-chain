package utils

import (
	sdkstoretypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func NewGasMeterWithMultiplier(ctx sdk.Context, limit uint64) sdk.GasMeter {
	if ctx.GasMeter() == nil {
		return sdk.NewGasMeter(limit)
	}
	n, d := ctx.GasMeter().Multiplier()
	return sdkstoretypes.NewMultiplierGasMeter(limit, n, d)
}

func NewInfiniteGasMeterWithMultiplier(ctx sdk.Context) sdk.GasMeter {
	if ctx.GasMeter() == nil {
		return sdk.NewInfiniteGasMeter()
	}
	n, d := ctx.GasMeter().Multiplier()
	return sdkstoretypes.NewInfiniteMultiplierGasMeter(n, d)
}
