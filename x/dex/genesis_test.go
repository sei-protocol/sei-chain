package dex_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/testutil/nullify"
	"github.com/sei-protocol/sei-chain/x/dex"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TEST_PAIR() types.Pair {
	return types.Pair{
		PriceDenom: "usdc",
		AssetDenom: "atom",
	}
}

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),

		LongBookList: []types.LongBook{
			{
				Price: sdk.ZeroDec(),
				Entry: &types.OrderEntry{
					Price: sdk.ZeroDec(),
				},
			},
			{
				Price: sdk.NewDec(1),
				Entry: &types.OrderEntry{
					Price: sdk.ZeroDec(),
				},
			},
		},
		ShortBookList: []types.ShortBook{
			{
				Price: sdk.ZeroDec(),
				Entry: &types.OrderEntry{
					Price: sdk.ZeroDec(),
				},
			},
			{
				Price: sdk.NewDec(1),
				Entry: &types.OrderEntry{
					Price: sdk.ZeroDec(),
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
