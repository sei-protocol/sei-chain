package antedecorators

import (
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

const (
	GasMultiplierNumerator                   uint64 = 1
	DefaultGasMultiplierDenominator          uint64 = 5
	WasmCorrectDependencyDiscountDenominator uint64 = 2
)

func GetGasMeterSetter(aclkeeper aclkeeper.Keeper) func(bool, sdk.Context, uint64, sdk.Tx) sdk.Context {
	return func(simulate bool, ctx sdk.Context, gasLimit uint64, tx sdk.Tx) sdk.Context {
		if simulate || ctx.BlockHeight() == 0 {
			return ctx.WithGasMeter(sdk.NewInfiniteGasMeter())
		}

		denominator := uint64(1)
		updatedGasDenominator := false
		for _, msg := range tx.GetMsgs() {
			candidateDenominator := getMessageMultiplierDenominator(ctx, msg, aclkeeper)
			if !updatedGasDenominator || candidateDenominator < denominator {
				updatedGasDenominator = true
				denominator = candidateDenominator
			}
		}
		return ctx.WithGasMeter(types.NewMultiplierGasMeter(gasLimit, DefaultGasMultiplierDenominator, denominator))
	}
}

func getMessageMultiplierDenominator(ctx sdk.Context, msg sdk.Msg, aclKeeper aclkeeper.Keeper) uint64 {
	// TODO: reason through whether it's reasonable to require non-* identifier for all operations
	//       under the context of inter-contract changes
	// only give gas discount if none of the dependency (except COMMIT) has id "*"
	if wasmExecuteMsg, ok := msg.(*wasmtypes.MsgExecuteContract); ok {
		msgInfo, err := acltypes.NewExecuteMessageInfo(wasmExecuteMsg.Msg)
		if err != nil {
			return DefaultGasMultiplierDenominator
		}
		if messageContainsNoWildcardDependencies(
			ctx,
			aclKeeper,
			wasmExecuteMsg.Contract,
			msgInfo,
			wasmExecuteMsg.Sender,
		) {
			return WasmCorrectDependencyDiscountDenominator * DefaultGasMultiplierDenominator
		}
	}
	return DefaultGasMultiplierDenominator
}

// TODO: add tracing to measure latency
func messageContainsNoWildcardDependencies(
	ctx sdk.Context,
	aclKeeper aclkeeper.Keeper,
	contractAddrStr string,
	msgInfo *acltypes.WasmMessageInfo,
	sender string,
) bool {
	addr, err := sdk.AccAddressFromBech32(contractAddrStr)
	if err != nil {
		return false
	}
	accessOps, err := aclKeeper.GetWasmDependencyAccessOps(ctx, addr, sender, msgInfo, make(aclkeeper.ContractReferenceLookupMap))
	if err != nil {
		return false
	}
	for _, op := range accessOps {
		if op.AccessType != sdkacltypes.AccessType_COMMIT && op.IdentifierTemplate == "*" {
			return false
		}
	}

	return true
}
