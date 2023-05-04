package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/testutil/nullify"
	"github.com/stretchr/testify/require"
)

func TestLongBookGet(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	items := keepertest.CreateNLongBook(keeper, ctx, 10)
	for i, item := range items {
		got, found := keeper.GetLongBookByPrice(ctx, keepertest.TestContract, sdk.NewDec(int64(i)), keepertest.TestPriceDenom, keepertest.TestAssetDenom)
		require.True(t, found)
		require.Equal(t,
			nullify.Fill(&item),
			nullify.Fill(&got),
		)
	}
}

func TestLongBookRemove(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	items := keepertest.CreateNLongBook(keeper, ctx, 10)
	for i := range items {
		keeper.RemoveLongBookByPrice(ctx, keepertest.TestContract, sdk.NewDec(int64(i)), keepertest.TestPriceDenom, keepertest.TestAssetDenom)
		_, found := keeper.GetLongBookByPrice(ctx, keepertest.TestContract, sdk.NewDec(int64(i)), keepertest.TestPriceDenom, keepertest.TestAssetDenom)
		require.False(t, found)
	}
}

func TestLongBookGetAll(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	items := keepertest.CreateNLongBook(keeper, ctx, 10)
	require.ElementsMatch(t,
		nullify.Fill(items),
		nullify.Fill(keeper.GetAllLongBook(ctx, keepertest.TestContract)),
	)
}
