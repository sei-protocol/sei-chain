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

const (
	TEST_CONTRACT    = "tc"
	TEST_PRICE_DENOM = "usdc"
	TEST_ASSET_DENOM = "atom"
)

func createNShortBook(keeper *keeper.Keeper, ctx sdk.Context, n int) []types.ShortBook {
	items := make([]types.ShortBook, n)
	for i := range items {
		items[i].Entry = &types.OrderEntry{
			Price:      sdk.NewDec(int64(i)),
			Quantity:   sdk.NewDec(int64(i)),
			PriceDenom: TEST_PRICE_DENOM,
			AssetDenom: TEST_ASSET_DENOM,
		}
		items[i].Price = sdk.NewDec(int64(i))
		keeper.SetShortBook(ctx, TEST_CONTRACT, items[i])
	}
	return items
}

func TestShortBookGet(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	items := createNShortBook(keeper, ctx, 10)
	for i, item := range items {
		got, found := keeper.GetShortBookByPrice(ctx, TEST_CONTRACT, sdk.NewDec(int64(i)), TEST_PRICE_DENOM, TEST_ASSET_DENOM)
		require.True(t, found)
		require.Equal(t,
			nullify.Fill(&item),
			nullify.Fill(&got),
		)
	}
}

func TestShortBookRemove(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	items := createNShortBook(keeper, ctx, 10)
	for i := range items {
		keeper.RemoveShortBookByPrice(ctx, TEST_CONTRACT, sdk.NewDec(int64(i)), TEST_PRICE_DENOM, TEST_ASSET_DENOM)
		_, found := keeper.GetShortBookByPrice(ctx, TEST_CONTRACT, sdk.NewDec(int64(i)), TEST_PRICE_DENOM, TEST_ASSET_DENOM)
		require.False(t, found)
	}
}

func TestShortBookGetAll(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	items := createNShortBook(keeper, ctx, 10)
	require.ElementsMatch(t,
		nullify.Fill(items),
		nullify.Fill(keeper.GetAllShortBook(ctx, TEST_CONTRACT)),
	)
}
