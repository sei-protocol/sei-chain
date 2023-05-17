package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

var TestEntryOne = types.OrderEntry{
	Price:      sdk.MustNewDecFromStr("10"),
	Quantity:   sdk.MustNewDecFromStr("3"),
	PriceDenom: keepertest.TestPriceDenom,
	AssetDenom: keepertest.TestAssetDenom,
	Allocations: []*types.Allocation{
		{
			Quantity: sdk.MustNewDecFromStr("2"),
			Account:  "abc",
			OrderId:  1,
		}, {
			Quantity: sdk.MustNewDecFromStr("1"),
			Account:  "def",
			OrderId:  2,
		},
	},
}

var TestEntryTwo = types.OrderEntry{
	Price:      sdk.MustNewDecFromStr("11"),
	Quantity:   sdk.MustNewDecFromStr("2"),
	PriceDenom: keepertest.TestPriceDenom,
	AssetDenom: keepertest.TestAssetDenom,
	Allocations: []*types.Allocation{
		{
			Quantity: sdk.MustNewDecFromStr("2"),
			Account:  "ghi",
			OrderId:  3,
		},
	},
}

func getCachedSortedOrderBookEntries(keeper *keeper.Keeper) *types.CachedSortedOrderBookEntries {
	loader := func(lctx sdk.Context, startExclusive sdk.Dec, withLimit bool) []types.OrderBookEntry {
		if !withLimit {
			return keeper.GetTopNLongBooksForPair(lctx, keepertest.TestContract, keepertest.TestPriceDenom, keepertest.TestAssetDenom, 1)
		}
		return keeper.GetTopNLongBooksForPairStarting(lctx, keepertest.TestContract, keepertest.TestPriceDenom, keepertest.TestAssetDenom, 1, startExclusive)
	}
	setter := func(lctx sdk.Context, o types.OrderBookEntry) {
		keeper.SetLongOrderBookEntry(lctx, keepertest.TestContract, o)
	}
	deleter := func(lctx sdk.Context, o types.OrderBookEntry) {
		keeper.RemoveLongBookByPrice(lctx, keepertest.TestContract, o.GetPrice(), keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	}
	return types.NewCachedSortedOrderBookEntries(loader, setter, deleter)
}

func populateEntries(ctx sdk.Context, keeper *keeper.Keeper) {
	keeper.SetLongBook(ctx, keepertest.TestContract, types.LongBook{
		Price: TestEntryOne.Price,
		Entry: &TestEntryOne,
	})
	keeper.SetLongBook(ctx, keepertest.TestContract, types.LongBook{
		Price: TestEntryTwo.Price,
		Entry: &TestEntryTwo,
	})
}

func TestIterateAndMutate(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	populateEntries(ctx, dexkeeper)
	cache := getCachedSortedOrderBookEntries(dexkeeper)
	// first Next should return entry two since its a higher priced long
	entry := cache.Next(ctx)
	require.NotNil(t, entry)
	require.Equal(t, TestEntryTwo.Price, entry.GetPrice())
	require.Equal(t, TestEntryTwo, *entry.GetOrderEntry())
	res, settled := cache.SettleQuantity(ctx, sdk.OneDec())
	require.Equal(t, []types.ToSettle{{
		Amount:  sdk.OneDec(),
		Account: "ghi",
		OrderID: 3,
	}}, res)
	require.Equal(t, sdk.OneDec(), settled)

	// second Next should still return entry two with decreased quantity
	entry = cache.Next(ctx)
	require.NotNil(t, entry)
	require.Equal(t, TestEntryTwo.Price, entry.GetPrice())
	require.Equal(t, sdk.OneDec(), entry.GetOrderEntry().Quantity)
	require.Equal(t, 1, len(entry.GetOrderEntry().Allocations))
	require.Equal(t, types.Allocation{
		Quantity: sdk.OneDec(),
		Account:  "ghi",
		OrderId:  3,
	}, *entry.GetOrderEntry().Allocations[0])
	res, settled = cache.SettleQuantity(ctx, sdk.OneDec())
	require.Equal(t, []types.ToSettle{{
		Amount:  sdk.OneDec(),
		Account: "ghi",
		OrderID: 3,
	}}, res)
	require.Equal(t, sdk.OneDec(), settled)

	// third Next should return entry one since entry two has fully settled (whether flushed or not)
	entry = cache.Next(ctx)
	require.NotNil(t, entry)
	require.Equal(t, TestEntryOne.Price, entry.GetPrice())
	require.Equal(t, TestEntryOne, *entry.GetOrderEntry())
	res, settled = cache.SettleQuantity(ctx, sdk.NewDec(4))
	require.Equal(t, []types.ToSettle{{
		Amount:  sdk.NewDec(2),
		Account: "abc",
		OrderID: 1,
	}, {
		Amount:  sdk.NewDec(1),
		Account: "def",
		OrderID: 2,
	}}, res)
	require.Equal(t, sdk.NewDec(3), settled)

	// fourth Next should return nil
	entry = cache.Next(ctx)
	require.Nil(t, entry)
}

func TestRefreshAndFlush(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	populateEntries(ctx, dexkeeper)
	cache := getCachedSortedOrderBookEntries(dexkeeper)
	_ = cache.Next(ctx)
	_, _ = cache.SettleQuantity(ctx, sdk.OneDec())
	// refresh without flushing settle changes should undo the settle
	cache.Refresh(ctx)
	entry := cache.Next(ctx)
	require.NotNil(t, entry)
	require.Equal(t, TestEntryTwo.Price, entry.GetPrice())
	require.Equal(t, TestEntryTwo, *entry.GetOrderEntry())

	_, _ = cache.SettleQuantity(ctx, sdk.OneDec())
	cache.Flush(ctx)
	// refresh after flushing should reflect the settle change
	cache.Refresh(ctx)
	entry = cache.Next(ctx)
	require.NotNil(t, entry)
	require.Equal(t, TestEntryTwo.Price, entry.GetPrice())
	require.Equal(t, sdk.OneDec(), entry.GetOrderEntry().Quantity)
	require.Equal(t, 1, len(entry.GetOrderEntry().Allocations))
	require.Equal(t, types.Allocation{
		Quantity: sdk.OneDec(),
		Account:  "ghi",
		OrderId:  3,
	}, *entry.GetOrderEntry().Allocations[0])

	_, _ = cache.SettleQuantity(ctx, sdk.OneDec())
	cache.Flush(ctx)
	// refresh after flushing a deleted entry should result in the next Next being the next entry
	cache.Refresh(ctx)
	entry = cache.Next(ctx)
	require.NotNil(t, entry)
	require.Equal(t, TestEntryOne.Price, entry.GetPrice())
	require.Equal(t, TestEntryOne, *entry.GetOrderEntry())
}
