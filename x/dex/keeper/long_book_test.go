package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/testutil/nullify"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func createNLongBook(keeper *keeper.Keeper, ctx sdk.Context, n int) []types.LongBook {
	items := make([]types.LongBook, n)
	for i := range items {
		items[i].Entry = &types.OrderEntry{
			Price:      sdk.NewDec(int64(i)),
			Quantity:   sdk.NewDec(int64(i)),
			PriceDenom: "USDC",
			AssetDenom: "ATOM",
		}
		items[i].Price = sdk.NewDec(int64(i))
		keeper.SetLongBook(ctx, TEST_CONTRACT, items[i])
	}
	return items
}

func TestLongBookGet(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	items := createNLongBook(keeper, ctx, 10)
	for i, item := range items {
		got, found := keeper.GetLongBookByPrice(ctx, TEST_CONTRACT, sdk.NewDec(int64(i)), "USDC", "ATOM")
		require.True(t, found)
		require.Equal(t,
			nullify.Fill(&item),
			nullify.Fill(&got),
		)
	}
}

func TestLongBookRemove(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	items := createNLongBook(keeper, ctx, 10)
	for i := range items {
		keeper.RemoveLongBookByPrice(ctx, TEST_CONTRACT, sdk.NewDec(int64(i)), "USDC", "ATOM")
		_, found := keeper.GetLongBookByPrice(ctx, TEST_CONTRACT, sdk.NewDec(int64(i)), "USDC", "ATOM")
		require.False(t, found)
	}
}

func TestLongBookGetAll(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	items := createNLongBook(keeper, ctx, 10)
	require.ElementsMatch(t,
		nullify.Fill(items),
		nullify.Fill(keeper.GetAllLongBook(ctx, TEST_CONTRACT)),
	)
}
