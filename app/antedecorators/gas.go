package antedecorators

import (
	"github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
)

func GetGasMeterSetter(pk paramskeeper.Keeper) func(bool, sdk.Context, uint64, sdk.Tx) sdk.Context {
	return func(simulate bool, ctx sdk.Context, gasLimit uint64, tx sdk.Tx) sdk.Context {
		cosmosGasParams := pk.GetCosmosGasParams(ctx)

		// In simulation, still use multiplier but with infinite gas limit
		if simulate || ctx.BlockHeight() == 0 {
			return ctx.WithGasMeter(types.NewInfiniteMultiplierGasMeter(cosmosGasParams.CosmosGasMultiplierNumerator, cosmosGasParams.CosmosGasMultiplierDenominator))
		}

		return ctx.WithGasMeter(types.NewMultiplierGasMeter(gasLimit, cosmosGasParams.CosmosGasMultiplierNumerator, cosmosGasParams.CosmosGasMultiplierDenominator))
	}
}
