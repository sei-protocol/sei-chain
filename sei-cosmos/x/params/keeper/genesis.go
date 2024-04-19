package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/params/types"
)

// InitGenesis new mint genesis.
func (k Keeper) InitGenesis(ctx sdk.Context, data *types.GenesisState) {
	k.SetFeesParams(ctx, data.FeesParams)
	k.SetCosmosGasParams(ctx, data.CosmosGasParams)
}

// ExportGenesis returns a GenesisState for a given context and keeper.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	feesParams := k.GetFeesParams(ctx)
	cosmosGasParams := k.GetCosmosGasParams(ctx)
	return types.NewGenesisState(feesParams, cosmosGasParams)
}
