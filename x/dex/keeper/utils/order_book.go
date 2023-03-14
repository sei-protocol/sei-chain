package utils

import (
	"sort"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dextypesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
)

func PopulateOrderbook(
	ctx sdk.Context,
	keeper *keeper.Keeper,
	contractAddr dextypesutils.ContractAddress,
	pair types.Pair,
) *types.OrderBook {
	longs := keeper.GetAllLongBookForPair(ctx, string(contractAddr), pair.PriceDenom, pair.AssetDenom)
	shorts := keeper.GetAllShortBookForPair(ctx, string(contractAddr), pair.PriceDenom, pair.AssetDenom)
	sortOrderBookEntries(longs)
	sortOrderBookEntries(shorts)
	return &types.OrderBook{
		Longs: &types.CachedSortedOrderBookEntries{
			Entries:      longs,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
		Shorts: &types.CachedSortedOrderBookEntries{
			Entries:      shorts,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
	}
}

func PopulateAllOrderbooks(
	ctx sdk.Context,
	keeper *keeper.Keeper,
	contractsAndPairs map[string][]types.Pair,
) *datastructures.TypedNestedSyncMap[string, dextypesutils.PairString, *types.OrderBook] {
	var orderBooks = datastructures.NewTypedNestedSyncMap[string, dextypesutils.PairString, *types.OrderBook]()
	wg := sync.WaitGroup{}
	for contractAddr, pairs := range contractsAndPairs {
		for _, pair := range pairs {
			wg.Add(1)
			go func(contractAddr string, pair types.Pair) {
				defer wg.Done()
				orderBook := PopulateOrderbook(ctx, keeper, dextypesutils.ContractAddress(contractAddr), pair)
				orderBooks.StoreNested(contractAddr, dextypesutils.GetPairString(&pair), orderBook)
			}(contractAddr, pair)
		}
	}
	wg.Wait()
	return orderBooks
}

func sortOrderBookEntries(entries []types.OrderBookEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].GetPrice().LT(entries[j].GetPrice())
	})
}

func FlushOrderbook(
	ctx sdk.Context,
	keeper *keeper.Keeper,
	typedContractAddr dextypesutils.ContractAddress,
	orderbook *types.OrderBook,
) {
	contractAddr := string(typedContractAddr)
	orderbook.Longs.DirtyEntries.DeepApply(func(entry types.OrderBookEntry) {
		if entry.GetEntry().Quantity.IsZero() {
			keeper.RemoveLongBookByPrice(ctx, contractAddr, entry.GetEntry().Price, entry.GetEntry().PriceDenom, entry.GetEntry().AssetDenom)
		} else {
			longOrder := entry.(*types.LongBook)
			keeper.SetLongBook(ctx, contractAddr, *longOrder)
		}
	})
	orderbook.Shorts.DirtyEntries.DeepApply(func(entry types.OrderBookEntry) {
		if entry.GetEntry().Quantity.IsZero() {
			keeper.RemoveShortBookByPrice(ctx, contractAddr, entry.GetEntry().Price, entry.GetEntry().PriceDenom, entry.GetEntry().AssetDenom)
		} else {
			shortOrder := entry.(*types.ShortBook)
			keeper.SetShortBook(ctx, contractAddr, *shortOrder)
		}
	})
}
