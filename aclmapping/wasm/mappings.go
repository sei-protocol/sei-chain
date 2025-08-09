package aclwasmmapping

import (
	"fmt"

	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkacltypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/accesscontrol"
	aclkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/accesscontrol/keeper"
	acltypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/accesscontrol/types"
)

var (
	ErrInvalidWasmExecuteMessage        = fmt.Errorf("invalid message received for type WasmExecuteContract")
	ErrInvalidWasmFunction              = fmt.Errorf("unable to identify wasm function")
	ErrWasmFunctionDependenciesDisabled = fmt.Errorf("wasm function dependency mapping disabled")
)

type WasmDependencyGenerator struct{}

func NewWasmDependencyGenerator() WasmDependencyGenerator {
	return WasmDependencyGenerator{}
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
	// TODO: need to test how errors from here affect the disabling of wasm execute dynamic dependencies
	msgInfo, err := acltypes.NewExecuteMessageInfo(executeContractMsg.Msg)
	if err != nil {
		return []sdkacltypes.AccessOperation{}, err
	}
	wasmAccessOps, err := keeper.GetWasmDependencyAccessOps(ctx, contractAddr, executeContractMsg.Sender, msgInfo, make(aclkeeper.ContractReferenceLookupMap))
	if err != nil {
		return []sdkacltypes.AccessOperation{}, err
	}
	return wasmAccessOps, nil
}
