package antedecorators

import (
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
)

const (
	WasmCorrectDependencyDiscountNumerator   uint64 = 1
	WasmCorrectDependencyDiscountDenominator uint64 = 2
)

func GetGasMeterSetter(aclkeeper aclkeeper.Keeper) func(bool, sdk.Context, uint64, sdk.Tx) sdk.Context {
	return func(simulate bool, ctx sdk.Context, gasLimit uint64, tx sdk.Tx) sdk.Context {
		if simulate || ctx.BlockHeight() == 0 {
			return ctx.WithGasMeter(sdk.NewInfiniteGasMeter())
		}

		numerator, denominator := uint64(0), uint64(1)
		for _, msg := range tx.GetMsgs() {
			candidateNumerator, candidateDenominator := getMessageMultiplier(ctx, msg, aclkeeper)
			numerator, denominator = maxMultiplier(numerator, denominator, candidateNumerator, candidateDenominator)
		}
		return ctx.WithGasMeter(types.NewMultiplierGasMeter(gasLimit, numerator, denominator))
	}
}

func getMessageMultiplier(ctx sdk.Context, msg sdk.Msg, aclkeeper aclkeeper.Keeper) (uint64, uint64) {
	if wasmExecuteMsg, ok := msg.(*wasmtypes.MsgExecuteContract); ok {
		addr, err := sdk.AccAddressFromBech32(wasmExecuteMsg.Contract)
		if err != nil {
			return 1, 1
		}
		mapping, err := aclkeeper.GetWasmDependencyMapping(ctx, addr, []byte{}, false)
		if err != nil {
			return 1, 1
		}
		// only give gas discount if none of the dependency (except COMMIT) has id "*"
		for _, op := range mapping.AccessOps {
			if op.Operation.AccessType != sdkacltypes.AccessType_COMMIT && op.Operation.IdentifierTemplate == "*" {
				return 1, 1
			}
		}
		return WasmCorrectDependencyDiscountNumerator, WasmCorrectDependencyDiscountDenominator
	}
	return 1, 1
}

func maxMultiplier(n1 uint64, d1 uint64, n2 uint64, d2 uint64) (uint64, uint64) {
	if n1*d2 < d1*n2 {
		return n2, d2
	}
	return n1, d1
}
