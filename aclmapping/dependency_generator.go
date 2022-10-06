package aclmapping

import (
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acldexmapping "github.com/sei-protocol/sei-chain/aclmapping/dex"
	aclwasmmapping "github.com/sei-protocol/sei-chain/aclmapping/wasm"
)

func CustomDependencyGenerator() aclkeeper.DependencyGeneratorMap {
	dependencyGeneratorMap := make(aclkeeper.DependencyGeneratorMap)

	dependencyGeneratorMap.Merge(acldexmapping.GetDexDependencyGenerators())
	dependencyGeneratorMap.Merge(aclwasmmapping.GetWasmDependencyGenerators())

	return dependencyGeneratorMap
}
