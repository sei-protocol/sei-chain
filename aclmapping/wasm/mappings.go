package aclwasmmapping

import (
	"encoding/json"
	"fmt"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

var (
	ErrInvalidWasmExecuteMessage        = fmt.Errorf("invalid message received for type WasmExecuteContract")
	ErrInvalidWasmFunction              = fmt.Errorf("unable to identify wasm function")
	ErrWasmFunctionDependenciesDisabled = fmt.Errorf("wasm function dependency mapping disabled")
)

type WasmDependencyGenerator struct {
	WasmKeeper wasmkeeper.Keeper
}

func NewWasmDependencyGenerator(wasmKeeper wasmkeeper.Keeper) WasmDependencyGenerator {
	return WasmDependencyGenerator{
		WasmKeeper: wasmKeeper,
	}
}

func (wasmDepGen WasmDependencyGenerator) GetWasmDependencyGenerators() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	// wasm execute
	executeContractKey := acltypes.GenerateMessageKey(&wasmtypes.MsgExecuteContract{})
	dependencyGeneratorMap[executeContractKey] = wasmDepGen.WasmExecuteContractGenerator

	return dependencyGeneratorMap
}

func (wasmDepGen WasmDependencyGenerator) WasmExecuteContractGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	executeContractMsg, ok := msg.(*wasmtypes.MsgExecuteContract)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrInvalidWasmExecuteMessage
	}
	contractAddr, err := sdk.AccAddressFromBech32(executeContractMsg.Contract)
	if err != nil {
		return []sdkacltypes.AccessOperation{}, err
	}
	contractInfo := wasmDepGen.WasmKeeper.GetContractInfo(ctx, contractAddr)
	codeID := contractInfo.CodeID

	jsonObj := make(map[string]interface{})
	jsonErr := json.Unmarshal(executeContractMsg.Msg, &jsonObj)
	var wasmFunction string
	if jsonErr != nil {
		// try unmarshalling to string for execute function with no params
		jsonErr2 := json.Unmarshal(executeContractMsg.Msg, &wasmFunction)
		if jsonErr2 != nil {
			return []sdkacltypes.AccessOperation{}, ErrInvalidWasmFunction
		}
	} else {
		if len(jsonObj) != 1 {
			return []sdkacltypes.AccessOperation{}, ErrInvalidWasmFunction
		}
		for fieldName := range jsonObj {
			// this should only run once based on the check above
			wasmFunction = fieldName
		}
	}
	wasmDependencyMapping, err := keeper.GetWasmFunctionDependencyMapping(ctx, codeID, wasmFunction)
	if err != nil {
		return []sdkacltypes.AccessOperation{}, err
	}
	if !wasmDependencyMapping.Enabled {
		return []sdkacltypes.AccessOperation{}, ErrWasmFunctionDependenciesDisabled
	}
	return wasmDependencyMapping.AccessOps, nil
}
