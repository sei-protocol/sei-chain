package antedecorators

import (
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
)

const (
	GasMultiplierNumerator                   uint64 = 1
	DefaultGasMultiplierDenominator          uint64 = 1
	WasmCorrectDependencyDiscountDenominator uint64 = 2
)

func GetGasMeterSetter(aclkeeper aclkeeper.Keeper) func(bool, sdk.Context, uint64, sdk.Tx) sdk.Context {
	return func(simulate bool, ctx sdk.Context, gasLimit uint64, tx sdk.Tx) sdk.Context {
		if simulate || ctx.BlockHeight() == 0 {
			return ctx.WithGasMeter(sdk.NewInfiniteGasMeter())
		}

		denominator := uint64(1)
		for _, msg := range tx.GetMsgs() {
			candidateDenominator := getMessageMultiplierDenominator(ctx, msg, aclkeeper)
			if candidateDenominator > denominator {
				denominator = candidateDenominator
			}
		}
		return ctx.WithGasMeter(types.NewMultiplierGasMeter(gasLimit, DefaultGasMultiplierDenominator, denominator))
	}
}

func getMessageMultiplierDenominator(ctx sdk.Context, msg sdk.Msg, aclkeeper aclkeeper.Keeper) uint64 {
	if wasmExecuteMsg, ok := msg.(*wasmtypes.MsgExecuteContract); ok {
		addr, err := sdk.AccAddressFromBech32(wasmExecuteMsg.Contract)
		if err != nil {
			return DefaultGasMultiplierDenominator
		}
		mapping, err := aclkeeper.GetWasmDependencyMapping(ctx, addr, []byte{}, false)
		if err != nil {
			return DefaultGasMultiplierDenominator
		}
		// only give gas discount if none of the dependency (except COMMIT) has id "*"
		for _, op := range mapping.AccessOps {
			if op.Operation.AccessType != sdkacltypes.AccessType_COMMIT && op.Operation.IdentifierTemplate == "*" {
				return DefaultGasMultiplierDenominator
			}
		}
		return WasmCorrectDependencyDiscountDenominator
	}
	return DefaultGasMultiplierDenominator
}
