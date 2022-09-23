package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

func (k Keeper) InitGenesis(ctx sdk.Context, genState types.GenesisState) {
	k.SetParams(ctx, genState.Params)
	for _, resourceDepedencyMapping := range genState.GetMessageDependencyMapping() {
		k.SetResourceDependencyMapping(ctx, resourceDepedencyMapping)
	}
}

func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	resourceDepedencyMappings := []types.MessageDependencyMapping{}
	k.IterateResourceKeys(ctx, func(dependencyMapping types.MessageDependencyMapping) (stop bool) {
		resourceDepedencyMappings = append(resourceDepedencyMappings, dependencyMapping)
		return false
	})
	return &types.GenesisState{
		Params:                   k.GetParams(ctx),
		MessageDependencyMapping: resourceDepedencyMappings,
	}
}
