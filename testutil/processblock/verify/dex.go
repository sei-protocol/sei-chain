package verify

import (
	"sort"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	dexkeeper "github.com/sei-protocol/sei-chain/x/dex/keeper"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func DexOrders(t *testing.T, ctx sdk.Context, keeper *dexkeeper.Keeper, f BlockRunnable, msgs []sdk.Msg) BlockRunnable {
	return func() []uint32 {
		orderPlacementsByMarket := map[string][]*dextypes.Order{}
		orderCancellationsByMarket := map[string][]*dextypes.Cancellation{}
		markets := map[string]struct{}{}
		for _, msg := range msgs {
			switch m := msg.(type) {
			case *dextypes.MsgPlaceOrders:
				for _, o := range m.Orders {
					id := strings.Join([]string{m.ContractAddr, o.PriceDenom, o.AssetDenom}, ",")
					markets[id] = struct{}{}
					if orders, ok := orderPlacementsByMarket[id]; ok {
						o.Id = keeper.GetNextOrderID(ctx, m.ContractAddr) + uint64(len(orders))
						orderPlacementsByMarket[id] = append(orders, o)
					} else {
						orderPlacementsByMarket[id] = []*dextypes.Order{o}
					}
				}
			case *dextypes.MsgCancelOrders:
				for _, o := range m.Cancellations {
					id := strings.Join([]string{m.ContractAddr, o.PriceDenom, o.AssetDenom}, ",")
					markets[id] = struct{}{}
					if cancels, ok := orderCancellationsByMarket[id]; ok {
						orderCancellationsByMarket[id] = append(cancels, o)
					} else {
						orderCancellationsByMarket[id] = []*dextypes.Cancellation{o}
					}
				}
			default:
				continue
			}
		}
		expectedLongBookByMarket := map[string]map[sdk.Dec]*dextypes.OrderEntry{}
		expectedShortBookByMarket := map[string]map[sdk.Dec]*dextypes.OrderEntry{}
		for market := range markets {
			orderPlacements := []*dextypes.Order{}
			if o, ok := orderPlacementsByMarket[market]; ok {
				orderPlacements = o
			}
			orderCancellations := []*dextypes.Cancellation{}
			if c, ok := orderCancellationsByMarket[market]; ok {
				orderCancellations = c
			}
			parts := strings.Split(market, ",")
			longBook, shortBook := expectedOrdersForMarket(ctx, keeper, orderPlacements, orderCancellations, parts[0], parts[1], parts[2])
			expectedLongBookByMarket[market] = longBook
			expectedShortBookByMarket[market] = shortBook
		}

		results := f()

		for market, longBook := range expectedLongBookByMarket {
			parts := strings.Split(market, ",")
			contract := parts[0]
			priceDenom := parts[1]
			assetDenom := parts[2]
			require.Equal(t, len(longBook), len(keeper.GetAllLongBookForPair(ctx, contract, priceDenom, assetDenom)))
			for price, entry := range longBook {
				actual, found := keeper.GetLongOrderBookEntryByPrice(ctx, contract, price, priceDenom, assetDenom)
				require.True(t, found)
				require.Equal(t, *entry, *(actual.GetOrderEntry()))
			}
		}

		for market, shortBook := range expectedShortBookByMarket {
			parts := strings.Split(market, ",")
			contract := parts[0]
			priceDenom := parts[1]
			assetDenom := parts[2]
			require.Equal(t, len(shortBook), len(keeper.GetAllShortBookForPair(ctx, contract, priceDenom, assetDenom)))
			for price, entry := range shortBook {
				actual, found := keeper.GetShortOrderBookEntryByPrice(ctx, contract, price, priceDenom, assetDenom)
				require.True(t, found)
				require.Equal(t, *entry, *(actual.GetOrderEntry()))
			}
		}

		return results
	}
}

func expectedOrdersForMarket(
	ctx sdk.Context,
	keeper *dexkeeper.Keeper,
	orderPlacements []*dextypes.Order,
	orderCancellations []*dextypes.Cancellation,
	contract string,
	priceDenom string,
	assetDenom string,
) (longEntries map[sdk.Dec]*dextypes.OrderEntry, shortEntries map[sdk.Dec]*dextypes.OrderEntry) {
	longBook := toOrderBookMap(keeper.GetAllLongBookForPair(ctx, contract, priceDenom, assetDenom))
	shortBook := toOrderBookMap(keeper.GetAllShortBookForPair(ctx, contract, priceDenom, assetDenom))
	getBook := func(d dextypes.PositionDirection) (book map[sdk.Dec]*dextypes.OrderEntry) {
		if d == dextypes.PositionDirection_LONG {
			book = longBook
		} else {
			book = shortBook
		}
		return
	}
	// first, cancellation
	for _, cancel := range orderCancellations {
		book := getBook(cancel.PositionDirection)
		if entry, ok := book[cancel.Price]; ok {
			entry.Allocations = removeMatched(entry.Allocations, func(a *dextypes.Allocation) bool { return a.OrderId == cancel.Id })
			updateEntryQuantity(entry)
			if entry.Quantity.IsZero() {
				delete(book, cancel.Price)
			}
		}
	}
	// then add new limit orders to book
	for _, o := range orderPlacements {
		if o.OrderType != dextypes.OrderType_LIMIT {
			continue
		}
		book := getBook(o.PositionDirection)
		if entry, ok := book[o.Price]; ok {
			entry.Allocations = append(entry.Allocations, &dextypes.Allocation{
				Account:  o.Account,
				Quantity: o.Quantity,
				OrderId:  o.Id,
			})
			updateEntryQuantity(entry)
		} else {
			book[o.Price] = &dextypes.OrderEntry{
				Price:      o.Price,
				Quantity:   o.Quantity,
				PriceDenom: priceDenom,
				AssetDenom: assetDenom,
				Allocations: []*dextypes.Allocation{{
					Account:  o.Account,
					Quantity: o.Quantity,
					OrderId:  o.Id,
				}},
			}
		}
	}
	// then match market orders
	marketBuys := utils.Filter(orderPlacements, func(o *dextypes.Order) bool {
		return o.OrderType == dextypes.OrderType_MARKET && o.PositionDirection == dextypes.PositionDirection_LONG
	})
	sort.SliceStable(marketBuys, func(i, j int) bool { return marketBuys[i].Price.GT(marketBuys[j].Price) })
	limitSellPrices := sortedPrices(shortBook, false)
	for i, j := 0, 0; i < len(marketBuys) && j < len(limitSellPrices) && marketBuys[i].Price.GTE(limitSellPrices[j]); {
		entry := shortBook[limitSellPrices[j]]
		if marketBuys[i].Quantity.GT(entry.Quantity) {
			marketBuys[i].Quantity = marketBuys[i].Quantity.Sub(entry.Quantity)
			delete(shortBook, limitSellPrices[j])
			j++
		} else {
			takeLiquidity(entry, marketBuys[i].Quantity)
			marketBuys[i].Quantity = sdk.ZeroDec()
			i++
		}
	}
	marketSells := utils.Filter(orderPlacements, func(o *dextypes.Order) bool {
		return o.OrderType == dextypes.OrderType_MARKET && o.PositionDirection == dextypes.PositionDirection_SHORT
	})
	sort.SliceStable(marketSells, func(i, j int) bool { return marketBuys[i].Price.LT(marketBuys[j].Price) })
	limitBuyPrices := sortedPrices(longBook, true)
	for i, j := 0, 0; i < len(marketSells) && j < len(limitBuyPrices) && marketSells[i].Price.LTE(limitBuyPrices[j]); {
		entry := longBook[limitBuyPrices[j]]
		if marketSells[i].Quantity.GT(entry.Quantity) {
			marketSells[i].Quantity = marketSells[i].Quantity.Sub(entry.Quantity)
			delete(longBook, limitBuyPrices[j])
			j++
		} else {
			takeLiquidity(entry, marketSells[i].Quantity)
			marketSells[i].Quantity = sdk.ZeroDec()
			i++
		}
	}
	// finally match limit orders
	limitBuyPrices = sortedPrices(longBook, true)
	limitSellPrices = sortedPrices(shortBook, false)
	for i, j := 0, 0; i < len(limitBuyPrices) && j < len(limitSellPrices) && limitBuyPrices[i].GTE(limitSellPrices[j]); {
		buyEntry := longBook[limitBuyPrices[i]]
		sellEntry := shortBook[limitSellPrices[j]]
		if buyEntry.Quantity.GT(sellEntry.Quantity) {
			takeLiquidity(buyEntry, sellEntry.Quantity)
			delete(shortBook, limitSellPrices[j])
			j++
		} else {
			takeLiquidity(sellEntry, buyEntry.Quantity)
			delete(longBook, limitBuyPrices[i])
			i++
		}
	}
	return longBook, shortBook
}

func toOrderBookMap(book []dextypes.OrderBookEntry) map[sdk.Dec]*dextypes.OrderEntry {
	bookMap := map[sdk.Dec]*dextypes.OrderEntry{}
	for _, e := range book {
		bookMap[e.GetPrice()] = e.GetOrderEntry()
	}
	return bookMap
}

func updateEntryQuantity(entry *dextypes.OrderEntry) {
	entry.Quantity = utils.Reduce(
		entry.Allocations,
		func(a *dextypes.Allocation, q sdk.Dec) sdk.Dec { return q.Add(a.Quantity) },
		sdk.ZeroDec(),
	)
}

func takeLiquidity(entry *dextypes.OrderEntry, quantity sdk.Dec) {
	if quantity.GT(entry.Quantity) {
		panic("insufficient liquidity")
	}
	allocated := sdk.ZeroDec()
	newAllocations := []*dextypes.Allocation{}
	for _, a := range entry.Allocations {
		switch {
		case allocated.Equal(quantity):
			newAllocations = append(newAllocations, a)
		case allocated.Add(a.Quantity).GT(quantity):
			a.Quantity = a.Quantity.Sub(quantity.Sub(allocated))
			newAllocations = append(newAllocations, a)
			allocated = quantity
		default:
			allocated = allocated.Add(a.Quantity)
		}
	}
	entry.Allocations = newAllocations
	entry.Quantity = entry.Quantity.Sub(quantity)
}

func sortedPrices(book map[sdk.Dec]*dextypes.OrderEntry, descending bool) []sdk.Dec {
	prices := []sdk.Dec{}
	for p := range book {
		prices = append(prices, p)
	}
	if descending {
		sort.Slice(prices, func(i, j int) bool { return prices[i].GT(prices[j]) })
	} else {
		sort.Slice(prices, func(i, j int) bool { return prices[i].LT(prices[j]) })
	}
	return prices
}
