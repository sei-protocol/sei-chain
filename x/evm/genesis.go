package evm

import (
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func InitGenesis(ctx sdk.Context, k *keeper.Keeper, genState types.GenesisState) {
	k.InitGenesis(ctx, genState)
	k.SetParams(ctx, genState.Params)
}

func ExportGenesis(ctx sdk.Context, k *keeper.Keeper) *types.GenesisState {
	genesis := types.DefaultGenesis()
	genesis.Params = k.GetParams(ctx)

	return genesis
}

// GetGenesisStateFromAppState returns x/evm GenesisState given raw application
// genesis state.
func GetGenesisStateFromAppState(cdc codec.JSONCodec, appState map[string]json.RawMessage) *types.GenesisState {
	var genesisState types.GenesisState

	if appState[types.ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[types.ModuleName], &genesisState)
	}

	return &genesisState
}
