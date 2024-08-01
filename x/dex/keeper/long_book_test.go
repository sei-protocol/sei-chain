package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/testutil/nullify"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
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

func TestGetTopNLongBooksForPair(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	prices := []string{"9.99", "0.001", "90.0", "10", "10.01", "9.9", "9.0", "1"}
	for _, price := range prices {
		keeper.SetLongBook(ctx, keepertest.TestContract, types.LongBook{
			Price: sdk.MustNewDecFromStr(price),
			Entry: &types.OrderEntry{
				Price:      sdk.MustNewDecFromStr(price),
				PriceDenom: keepertest.TestPriceDenom,
				AssetDenom: keepertest.TestAssetDenom,
			},
		})
	}
	expected := []sdk.Dec{
		sdk.MustNewDecFromStr("90.0"),
		sdk.MustNewDecFromStr("10.01"),
		sdk.MustNewDecFromStr("10"),
		sdk.MustNewDecFromStr("9.99"),
		sdk.MustNewDecFromStr("9.9"),
		sdk.MustNewDecFromStr("9.0"),
		sdk.MustNewDecFromStr("1"),
		sdk.MustNewDecFromStr("0.001"),
	}
	loaded := keeper.GetTopNLongBooksForPair(ctx, keepertest.TestContract, keepertest.TestPriceDenom, keepertest.TestAssetDenom, 10)
	require.Equal(t, expected, utils.Map(loaded, func(b types.OrderBookEntry) sdk.Dec { return b.GetPrice() }))
}

func TestGetTopNLongBooksForPairStarting(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	prices := []string{"9.99", "0.001", "90.0", "10", "10.01", "9.9", "9.0", "1"}
	for _, price := range prices {
		keeper.SetLongBook(ctx, keepertest.TestContract, types.LongBook{
			Price: sdk.MustNewDecFromStr(price),
			Entry: &types.OrderEntry{
				Price:      sdk.MustNewDecFromStr(price),
				PriceDenom: keepertest.TestPriceDenom,
				AssetDenom: keepertest.TestAssetDenom,
			},
		})
	}
	expected := []sdk.Dec{
		sdk.MustNewDecFromStr("9.99"),
		sdk.MustNewDecFromStr("9.9"),
	}
	loaded := keeper.GetTopNLongBooksForPairStarting(ctx, keepertest.TestContract, keepertest.TestPriceDenom, keepertest.TestAssetDenom, 2, sdk.MustNewDecFromStr("9.999"))
	require.Equal(t, expected, utils.Map(loaded, func(b types.OrderBookEntry) sdk.Dec { return b.GetPrice() }))
}
