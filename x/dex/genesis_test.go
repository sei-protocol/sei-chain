package dex_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/testutil/nullify"
	"github.com/sei-protocol/sei-chain/x/dex"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TEST_PAIR() types.Pair {
	return types.Pair{
		PriceDenom: "ust",
		AssetDenom: "luna",
	}
}

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),

		LongBookList: []types.LongBook{
			{
				Id: 0,
				Entry: &types.OrderEntry{
					Price: 0,
				},
			},
			{
				Id: 1,
				Entry: &types.OrderEntry{
					Price: 0,
				},
			},
		},
		ShortBookList: []types.ShortBook{
			{
				Id: 0,
				Entry: &types.OrderEntry{
					Price: 0,
				},
			},
			{
				Id: 1,
				Entry: &types.OrderEntry{
					Price: 0,
				},
			},
		},
		// this line is used by starport scaffolding # genesis/test/state
	}

	k, ctx := keepertest.DexKeeper(t)
	dex.InitGenesis(ctx, *k, genesisState)
	got := dex.ExportGenesis(ctx, *k)
	require.NotNil(t, got)

	nullify.Fill(&genesisState)
	nullify.Fill(got)

	require.ElementsMatch(t, genesisState.LongBookList, got.LongBookList)
	require.ElementsMatch(t, genesisState.ShortBookList, got.ShortBookList)
	// this line is used by starport scaffolding # genesis/test/assert
}
