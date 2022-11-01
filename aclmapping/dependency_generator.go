package aclmapping

import (
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	aclbankmapping "github.com/sei-protocol/sei-chain/aclmapping/bank"
	acldexmapping "github.com/sei-protocol/sei-chain/aclmapping/dex"
	aclwasmmapping "github.com/sei-protocol/sei-chain/aclmapping/wasm"
)

type CustomDependencyGenerator struct{}

func NewCustomDependencyGenerator() CustomDependencyGenerator {
	return CustomDependencyGenerator{}
}

func (customDepGen CustomDependencyGenerator) GetCustomDependencyGenerators() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	dependencyGeneratorMap.Merge(acldexmapping.GetDexDependencyGenerators())
	dependencyGeneratorMap.Merge(aclbankmapping.GetBankDepedencyGenerator())
	wasmDependencyGenerators := aclwasmmapping.NewWasmDependencyGenerator()
	dependencyGeneratorMap.Merge(wasmDependencyGenerators.GetWasmDependencyGenerators())

	return dependencyGeneratorMap
}
