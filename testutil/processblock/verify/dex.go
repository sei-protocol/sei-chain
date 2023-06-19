package verify

import (
	"sort"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/utils"
	dexkeeper "github.com/sei-protocol/sei-chain/x/dex/keeper"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func DexOrders(t *testing.T, app *processblock.App, f BlockRunnable, txs []signing.Tx) BlockRunnable {
	return func() []uint32 {
		orderPlacementsByMarket := map[string][]*dextypes.Order{}
		orderCancellationsByMarket := map[string][]*dextypes.Cancellation{}
		markets := map[string]struct{}{}
		for _, tx := range txs {
			for _, msg := range tx.GetMsgs() {
				switch m := msg.(type) {
				case *dextypes.MsgPlaceOrders:
					for _, o := range m.Orders {
						id := strings.Join([]string{m.ContractAddr, o.PriceDenom, o.AssetDenom}, ",")
						markets[id] = struct{}{}
						if orders, ok := orderPlacementsByMarket[id]; ok {
							o.Id = app.DexKeeper.GetNextOrderID(app.Ctx(), m.ContractAddr) + uint64(len(orders))
							orderPlacementsByMarket[id] = append(orders, o)
						} else {
							o.Id = app.DexKeeper.GetNextOrderID(app.Ctx(), m.ContractAddr)
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
		}
		expectedLongBookByMarket := map[string]map[string]*dextypes.OrderEntry{}
		expectedShortBookByMarket := map[string]map[string]*dextypes.OrderEntry{}
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
			longBook, shortBook := expectedOrdersForMarket(app.Ctx(), &app.DexKeeper, orderPlacements, orderCancellations, parts[0], parts[1], parts[2])
			expectedLongBookByMarket[market] = longBook
			expectedShortBookByMarket[market] = shortBook
		}

		results := f()

		for market, longBook := range expectedLongBookByMarket {
			parts := strings.Split(market, ",")
			contract := parts[0]
			priceDenom := parts[1]
			assetDenom := parts[2]
			require.Equal(t, len(longBook), len(app.DexKeeper.GetAllLongBookForPair(app.Ctx(), contract, priceDenom, assetDenom)))
			for price, entry := range longBook {
				actual, found := app.DexKeeper.GetLongOrderBookEntryByPrice(app.Ctx(), contract, sdk.MustNewDecFromStr(price), priceDenom, assetDenom)
				require.True(t, found)
				require.Equal(t, *entry, *(actual.GetOrderEntry()))
			}
		}

		for market, shortBook := range expectedShortBookByMarket {
			parts := strings.Split(market, ",")
			contract := parts[0]
			priceDenom := parts[1]
			assetDenom := parts[2]
			require.Equal(t, len(shortBook), len(app.DexKeeper.GetAllShortBookForPair(app.Ctx(), contract, priceDenom, assetDenom)))
			for price, entry := range shortBook {
				actual, found := app.DexKeeper.GetShortOrderBookEntryByPrice(app.Ctx(), contract, sdk.MustNewDecFromStr(price), priceDenom, assetDenom)
				require.True(t, found)
				require.Equal(t, *entry, *(actual.GetOrderEntry()))
			}
		}

		return results
	}
}

// A slow but correct implementation of dex exchange logics that build the expected order book state based on the
// current state and the list of new orders/cancellations, based on dex exchange rules.
func expectedOrdersForMarket(
	ctx sdk.Context,
	keeper *dexkeeper.Keeper,
	orderPlacements []*dextypes.Order,
	orderCancellations []*dextypes.Cancellation,
	contract string,
	priceDenom string,
	assetDenom string,
) (longEntries map[string]*dextypes.OrderEntry, shortEntries map[string]*dextypes.OrderEntry) {
	longBook := toOrderBookMap(keeper.GetAllLongBookForPair(ctx, contract, priceDenom, assetDenom))
	shortBook := toOrderBookMap(keeper.GetAllShortBookForPair(ctx, contract, priceDenom, assetDenom))
	books := map[dextypes.PositionDirection]map[string]*dextypes.OrderEntry{
		dextypes.PositionDirection_LONG:  longBook,
		dextypes.PositionDirection_SHORT: shortBook,
	}
	// first, cancellation
	cancelOrders(books, orderCancellations)
	// then add new limit orders to book
	addOrders(books, orderPlacements)
	// then match market orders
	matchOrders(getMarketOrderBookMap(orderPlacements, dextypes.PositionDirection_LONG), shortBook)
	matchOrders(longBook, getMarketOrderBookMap(orderPlacements, dextypes.PositionDirection_SHORT))
	// finally match limit orders
	matchOrders(longBook, shortBook)
	return longBook, shortBook
}

func cancelOrders(
	books map[dextypes.PositionDirection]map[string]*dextypes.OrderEntry,
	orderCancellations []*dextypes.Cancellation,
) {
	for _, cancel := range orderCancellations {
		book := books[cancel.PositionDirection]
		if entry, ok := book[cancel.Price.String()]; ok {
			entry.Allocations = removeMatched(entry.Allocations, func(a *dextypes.Allocation) bool { return a.OrderId == cancel.Id })
			updateEntryQuantity(entry)
			if entry.Quantity.IsZero() {
				delete(book, cancel.Price.String())
			}
		}
	}
}

func addOrders(
	books map[dextypes.PositionDirection]map[string]*dextypes.OrderEntry,
	orderPlacements []*dextypes.Order,
) {
	for _, o := range orderPlacements {
		if o.OrderType != dextypes.OrderType_LIMIT {
			continue
		}
		book := books[o.PositionDirection]
		newAllocation := &dextypes.Allocation{
			Account:  o.Account,
			Quantity: o.Quantity,
			OrderId:  o.Id,
		}
		if entry, ok := book[o.Price.String()]; ok {
			entry.Allocations = append(entry.Allocations, newAllocation)
			updateEntryQuantity(entry)
		} else {
			book[o.Price.String()] = &dextypes.OrderEntry{
				Price:       o.Price,
				Quantity:    o.Quantity,
				PriceDenom:  o.PriceDenom,
				AssetDenom:  o.AssetDenom,
				Allocations: []*dextypes.Allocation{newAllocation},
			}
		}
	}
}

func matchOrders(
	longBook map[string]*dextypes.OrderEntry,
	shortBook map[string]*dextypes.OrderEntry,
) {
	buyPrices := sortedPrices(longBook, true)
	sellPrices := sortedPrices(shortBook, false)
	for i, j := 0, 0; i < len(buyPrices) && j < len(sellPrices) && buyPrices[i].GTE(sellPrices[j]); {
		buyEntry := longBook[buyPrices[i].String()]
		sellEntry := shortBook[sellPrices[j].String()]
		if buyEntry.Quantity.GT(sellEntry.Quantity) {
			takeLiquidity(longBook, buyPrices[i], sellEntry.Quantity)
			takeLiquidity(shortBook, sellPrices[i], sellEntry.Quantity)
			j++
		} else {
			takeLiquidity(longBook, buyPrices[i], buyEntry.Quantity)
			takeLiquidity(shortBook, sellPrices[i], buyEntry.Quantity)
			i++
		}
	}
}

func toOrderBookMap(book []dextypes.OrderBookEntry) map[string]*dextypes.OrderEntry {
	bookMap := map[string]*dextypes.OrderEntry{}
	for _, e := range book {
		bookMap[e.GetPrice().String()] = e.GetOrderEntry()
	}
	return bookMap
}

func getMarketOrderBookMap(orderPlacements []*dextypes.Order, direction dextypes.PositionDirection) map[string]*dextypes.OrderEntry {
	bookMap := map[string]*dextypes.OrderEntry{}
	for _, o := range orderPlacements {
		if o.OrderType != dextypes.OrderType_MARKET || o.PositionDirection != direction {
			continue
		}
		bookMap[o.Price.String()] = orderToOrderEntry(o)
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

func takeLiquidity(book map[string]*dextypes.OrderEntry, price sdk.Dec, quantity sdk.Dec) {
	entry := book[price.String()]
	if entry.Quantity.Equal(quantity) {
		delete(book, price.String())
		return
	}
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

func sortedPrices(book map[string]*dextypes.OrderEntry, descending bool) []sdk.Dec {
	prices := []sdk.Dec{}
	for p := range book {
		prices = append(prices, sdk.MustNewDecFromStr(p))
	}
	if descending {
		sort.Slice(prices, func(i, j int) bool { return prices[i].GT(prices[j]) })
	} else {
		sort.Slice(prices, func(i, j int) bool { return prices[i].LT(prices[j]) })
	}
	return prices
}

func orderToOrderEntry(order *dextypes.Order) *dextypes.OrderEntry {
	return &dextypes.OrderEntry{
		Price:       order.Price,
		Quantity:    order.Quantity,
		Allocations: []*dextypes.Allocation{{Quantity: order.Quantity}},
	}
}
