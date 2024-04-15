package antedecorators

import (
	"github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
)

const (
	GasMultiplierNumerator                   uint64 = 1
	DefaultGasMultiplierDenominator          uint64 = 4
	WasmCorrectDependencyDiscountDenominator uint64 = 2
)

func GetGasMeterSetter(aclkeeper aclkeeper.Keeper) func(bool, sdk.Context, uint64, sdk.Tx) sdk.Context {
	return func(simulate bool, ctx sdk.Context, gasLimit uint64, tx sdk.Tx) sdk.Context {
		if simulate || ctx.BlockHeight() == 0 {
			return ctx.WithGasMeter(sdk.NewInfiniteGasMeter())
		}

		return ctx.WithGasMeter(types.NewMultiplierGasMeter(gasLimit, GasMultiplierNumerator, DefaultGasMultiplierDenominator))
	}
}
