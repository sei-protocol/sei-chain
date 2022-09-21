package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/accesscontrol/types"
)

func (k Keeper) InitGenesis(ctx sdk.Context, genState types.GenesisState) {
	k.SetParams(ctx, genState.Params)
	for _, resourceDepedencyMapping := range genState.Params.GetMessageDependencyMapping() {
		k.SetResourceDepedencyMapping(ctx, resourceDepedencyMapping)
	}
}

func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	return &types.GenesisState{
		Params: k.GetParams(ctx),
	}
}
