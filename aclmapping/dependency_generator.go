package aclmapping

import (
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	aclbankmapping "github.com/sei-protocol/sei-chain/aclmapping/bank"
	acldexmapping "github.com/sei-protocol/sei-chain/aclmapping/dex"
	aclwasmmapping "github.com/sei-protocol/sei-chain/aclmapping/wasm"
)

type CustomDependencyGenerator struct {
	WasmKeeper wasmkeeper.Keeper
}

func NewCustomDependencyGenerator(wasmKeeper wasmkeeper.Keeper) CustomDependencyGenerator {
	return CustomDependencyGenerator{WasmKeeper: wasmKeeper}
}

func (customDepGen CustomDependencyGenerator) GetCustomDependencyGenerators() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	dependencyGeneratorMap.Merge(acldexmapping.GetDexDependencyGenerators())
	dependencyGeneratorMap.Merge(aclbankmapping.GetBankDepedencyGenerator())
	wasmDependencyGenerators := aclwasmmapping.NewWasmDependencyGenerator(customDepGen.WasmKeeper)
	dependencyGeneratorMap.Merge(wasmDependencyGenerators.GetWasmDependencyGenerators())

	return dependencyGeneratorMap
}
