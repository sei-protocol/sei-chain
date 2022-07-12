package dex

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// InitGenesis initializes the capability module's state from a provided genesis
// state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, genState types.GenesisState) {
	// Set all the longBook
	for _, elem := range genState.LongBookList {
		k.SetLongBook(ctx, "genesis", elem)
	}

	// Set all the shortBook
	for _, elem := range genState.ShortBookList {
		k.SetShortBook(ctx, "genesis", elem)
	}

	// Set initial tick size for each pair
	// tick size is the minimum unit that can be traded for certain pair
	for _, elem := range genState.TickSizeList {
		k.SetDefaultTickSizeForPair(ctx, *elem.Pair, elem.Ticksize)
	}

	// this line is used by starport scaffolding # genesis/module/init
	k.SetParams(ctx, genState.Params)

	k.SetEpoch(ctx, genState.LastEpoch)
}

// ExportGenesis returns the capability module's exported genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	genesis := types.DefaultGenesis()
	genesis.Params = k.GetParams(ctx)

	genesis.LongBookList = k.GetAllLongBook(ctx, "genesis")
	genesis.ShortBookList = k.GetAllShortBook(ctx, "genesis")
	// this line is used by starport scaffolding # genesis/module/export

	return genesis
}
