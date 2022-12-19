package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

func (k Keeper) InitGenesis(ctx sdk.Context, genState types.GenesisState) {
	k.SetParams(ctx, genState.Params)
	for _, resourceDependencyMapping := range genState.GetMessageDependencyMapping() {
		k.SetResourceDependencyMapping(ctx, resourceDependencyMapping)
	}
	for _, wasmDependencyMapping := range genState.GetWasmDependencyMappings() {
		k.SetWasmDependencyMapping(ctx, wasmDependencyMapping)
	}
}

func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	resourceDependencyMappings := []acltypes.MessageDependencyMapping{}
	k.IterateResourceKeys(ctx, func(dependencyMapping acltypes.MessageDependencyMapping) (stop bool) {
		resourceDependencyMappings = append(resourceDependencyMappings, dependencyMapping)
		return false
	})
	wasmDependencyMappings := []acltypes.WasmDependencyMapping{}
	k.IterateWasmDependencies(ctx, func(dependencyMapping acltypes.WasmDependencyMapping) (stop bool) {
		wasmDependencyMappings = append(wasmDependencyMappings, dependencyMapping)
		return false
	})
	return &types.GenesisState{
		Params:                   k.GetParams(ctx),
		MessageDependencyMapping: resourceDependencyMappings,
		WasmDependencyMappings:   wasmDependencyMappings,
	}
}
