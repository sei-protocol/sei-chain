package antedecorators

import (
	"fmt"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
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

func getMessageMultiplierDenominator(ctx sdk.Context, msg sdk.Msg, aclKeeper aclkeeper.Keeper) uint64 {
	// TODO: reason through whether it's reasonable to require non-* identifier for all operations
	//       under the context of inter-contract changes
	// only give gas discount if none of the dependency (except COMMIT) has id "*"
	if wasmExecuteMsg, ok := msg.(*wasmtypes.MsgExecuteContract); ok {
		msgName := ""
		msgInfo, err := acltypes.NewExecuteMessageInfo(wasmExecuteMsg.Msg)
		if err == nil {
			msgName = msgInfo.MessageName
		}
		if messageContainsNoWildcardDependencies(
			ctx,
			aclKeeper,
			wasmExecuteMsg.Contract,
			msgName,
			sdkacltypes.WasmMessageSubtype_EXECUTE,
			make(map[string]struct{}),
		) {
			return WasmCorrectDependencyDiscountDenominator
		}
	}
	return DefaultGasMultiplierDenominator
}

// TODO: add tracing to measure latency
func messageContainsNoWildcardDependencies(
	ctx sdk.Context,
	aclKeeper aclkeeper.Keeper,
	contractAddrStr string,
	messageName string,
	messageSubtype sdkacltypes.WasmMessageSubtype,
	cycleDetector map[string]struct{},
) bool {
	cycleIdentifier := fmt.Sprintf("%s:%s", contractAddrStr, messageName)
	if _, ok := cycleDetector[cycleIdentifier]; ok {
		// no need to raise an error here. Simply returning false since a contract-message
		// with cycle will not be parallelized anyway
		return false
	}
	cycleDetector[cycleIdentifier] = struct{}{}
	addr, err := sdk.AccAddressFromBech32(contractAddrStr)
	if err != nil {
		return false
	}
	mapping, err := aclKeeper.GetRawWasmDependencyMapping(ctx, addr)
	if err != nil {
		return false
	}
	// check base access operations
	for _, op := range mapping.BaseAccessOps {
		if op.Operation.AccessType != sdkacltypes.AccessType_COMMIT && op.Operation.IdentifierTemplate == "*" {
			return false
		}
	}
	// check message-specific access operations
	messageSpecificAccessOps := []*sdkacltypes.WasmAccessOperations{}
	if messageSubtype == sdkacltypes.WasmMessageSubtype_EXECUTE {
		messageSpecificAccessOps = mapping.ExecuteAccessOps
	} else if messageSubtype == sdkacltypes.WasmMessageSubtype_QUERY {
		messageSpecificAccessOps = mapping.QueryAccessOps
	}
	for _, ops := range messageSpecificAccessOps {
		if ops.MessageName != messageName {
			continue
		}
		for _, op := range ops.WasmOperations {
			if op.Operation.IdentifierTemplate == "*" {
				return false
			}
		}
	}
	// check base references
	for _, ref := range mapping.BaseContractReferences {
		if !messageContainsNoWildcardDependencies(
			ctx,
			aclKeeper,
			ref.ContractAddress,
			ref.MessageName,
			ref.MessageType,
			cycleDetector,
		) {
			return false
		}
	}
	messageSpecificReferences := []*sdkacltypes.WasmContractReferences{}
	if messageSubtype == sdkacltypes.WasmMessageSubtype_EXECUTE {
		messageSpecificReferences = mapping.ExecuteContractReferences
	} else if messageSubtype == sdkacltypes.WasmMessageSubtype_QUERY {
		messageSpecificReferences = mapping.QueryContractReferences
	}
	for _, refs := range messageSpecificReferences {
		if refs.MessageName != messageName {
			continue
		}
		for _, ref := range refs.ContractReferences {
			if !messageContainsNoWildcardDependencies(
				ctx,
				aclKeeper,
				ref.ContractAddress,
				ref.MessageName,
				ref.MessageType,
				cycleDetector,
			) {
				return false
			}
		}
	}

	return true
}
