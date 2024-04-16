package antedecorators

import (
	"github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func GetGasMeterSetter() func(bool, sdk.Context, uint64, sdk.Tx) sdk.Context {
	return func(simulate bool, ctx sdk.Context, gasLimit uint64, tx sdk.Tx) sdk.Context {
		if ctx.BlockHeight() == 0 {
			return ctx.WithGasMeter(sdk.NewInfiniteGasMeter())
		}

		consensusParams := ctx.ConsensusParams()
		if consensusParams == nil || consensusParams.Block == nil {
			panic("block params is nil")
		}

		return ctx.WithGasMeter(types.NewMultiplierGasMeter(gasLimit, consensusParams.Block.CosmosGasMultiplierNumerator, consensusParams.Block.CosmosGasMultiplierDenominator))
	}
}
