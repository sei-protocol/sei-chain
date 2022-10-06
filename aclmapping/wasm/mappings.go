package aclwasmmapping

import (
	"fmt"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

var (
	ErrPlaceOrdersGenerator = fmt.Errorf("invalid message received for type DexPlaceOrders")
)

func GetWasmDependencyGenerators() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	// wasm execute
	executeContractKey := acltypes.GenerateMessageKey(&wasmtypes.MsgExecuteContract{})
	dependencyGeneratorMap[executeContractKey] = WasmExecuteContractGenerator

	return dependencyGeneratorMap
}

func WasmExecuteContractGenerator(keeper aclkeeper.Keeper, ctx sdk.Context, msg sdk.Msg) ([]sdkacltypes.AccessOperation, error) {
	// TODO: implement
	_, ok := msg.(*wasmtypes.MsgExecuteContract)
	if !ok {
		return []sdkacltypes.AccessOperation{}, ErrPlaceOrdersGenerator
	}
	// get the mapping from accesscontrol module for
	return []sdkacltypes.AccessOperation{}, nil
}
