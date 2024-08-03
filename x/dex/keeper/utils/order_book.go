package utils

import (
	"fmt"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func PopulateOrderbook(
	ctx sdk.Context,
	keeper *keeper.Keeper,
	contractAddr types.ContractAddress,
	pair types.Pair,
) *types.OrderBook {
	// TODO update to param
	loadCnt := int(keeper.GetOrderBookEntriesPerLoad(ctx))
	longLoader := func(lctx sdk.Context, startExclusive sdk.Dec, withLimit bool) []types.OrderBookEntry {
		if !withLimit {
			return keeper.GetTopNLongBooksForPair(lctx, string(contractAddr), pair.PriceDenom, pair.AssetDenom, loadCnt)
		}
		return keeper.GetTopNLongBooksForPairStarting(lctx, string(contractAddr), pair.PriceDenom, pair.AssetDenom, loadCnt, startExclusive)
	}
	shortLoader := func(lctx sdk.Context, startExclusive sdk.Dec, withLimit bool) []types.OrderBookEntry {
		if !withLimit {
			return keeper.GetTopNShortBooksForPair(lctx, string(contractAddr), pair.PriceDenom, pair.AssetDenom, loadCnt)
		}
		return keeper.GetTopNShortBooksForPairStarting(lctx, string(contractAddr), pair.PriceDenom, pair.AssetDenom, loadCnt, startExclusive)
	}
	longSetter := func(lctx sdk.Context, o types.OrderBookEntry) {
		keeper.SetLongOrderBookEntry(lctx, string(contractAddr), o)
		err := keeper.SetOrderCount(lctx, string(contractAddr), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_LONG, o.GetPrice(), uint64(len(o.GetOrderEntry().GetAllocations())))
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error setting order count: %s", err))
		}
	}
	shortSetter := func(lctx sdk.Context, o types.OrderBookEntry) {
		keeper.SetShortOrderBookEntry(lctx, string(contractAddr), o)
		err := keeper.SetOrderCount(lctx, string(contractAddr), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_SHORT, o.GetPrice(), uint64(len(o.GetOrderEntry().GetAllocations())))
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error setting order count: %s", err))
		}
	}
	longDeleter := func(lctx sdk.Context, o types.OrderBookEntry) {
		keeper.RemoveLongBookByPrice(lctx, string(contractAddr), o.GetPrice(), pair.PriceDenom, pair.AssetDenom)
		err := keeper.SetOrderCount(lctx, string(contractAddr), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_LONG, o.GetPrice(), 0)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error setting order count: %s", err))
		}
	}
	shortDeleter := func(lctx sdk.Context, o types.OrderBookEntry) {
		keeper.RemoveShortBookByPrice(lctx, string(contractAddr), o.GetPrice(), pair.PriceDenom, pair.AssetDenom)
		err := keeper.SetOrderCount(lctx, string(contractAddr), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_SHORT, o.GetPrice(), 0)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error setting order count: %s", err))
		}
	}
	return &types.OrderBook{
		Contract: contractAddr,
		Pair:     pair,
		Longs:    types.NewCachedSortedOrderBookEntries(longLoader, longSetter, longDeleter),
		Shorts:   types.NewCachedSortedOrderBookEntries(shortLoader, shortSetter, shortDeleter),
	}
}

func PopulateAllOrderbooks(
	ctx sdk.Context,
	keeper *keeper.Keeper,
	contractsAndPairs map[string][]types.Pair,
) *datastructures.TypedNestedSyncMap[string, types.PairString, *types.OrderBook] {
	var orderBooks = datastructures.NewTypedNestedSyncMap[string, types.PairString, *types.OrderBook]()
	wg := sync.WaitGroup{}
	for contractAddr, pairs := range contractsAndPairs {
		orderBooks.Store(contractAddr, datastructures.NewTypedSyncMap[types.PairString, *types.OrderBook]())
		for _, pair := range pairs {
			wg.Add(1)
			go func(contractAddr string, pair types.Pair) {
				defer wg.Done()
				orderBook := PopulateOrderbook(ctx, keeper, types.ContractAddress(contractAddr), pair)
				orderBooks.StoreNested(contractAddr, types.GetPairString(&pair), orderBook)
			}(contractAddr, pair)
		}
	}
	wg.Wait()
	return orderBooks
}
