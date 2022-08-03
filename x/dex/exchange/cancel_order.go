package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func CancelOrders(
	cancels []types.Cancellation,
	orderbook *types.OrderBook,
	originalOrders map[uint64]types.Order,
) {
	for _, cancel := range cancels {
		if originalOrder, ok := originalOrders[cancel.Id]; ok {
			cancelOrder(originalOrder, orderbook.Longs)
			cancelOrder(originalOrder, orderbook.Shorts)
		}
	}
}

func cancelOrder(originalOrder types.Order, orderBookEntries *types.CachedSortedOrderBookEntries) {
	for _, order := range orderBookEntries.Entries {
		if !originalOrder.Price.Equal(order.GetPrice()) {
			continue
		}
		orderBookEntry := order.GetEntry()
		newAllocations := []*types.Allocation{}
		newQuantity := sdk.ZeroDec()
		for _, allocation := range orderBookEntry.Allocations {
			if allocation.OrderId != originalOrder.Id {
				newAllocations = append(newAllocations, allocation)
				newQuantity = newQuantity.Add(allocation.Quantity)
			} else {
				// `Add` is idempotent
				orderBookEntries.AddDirtyEntry(order)
			}
		}
		orderBookEntry.Quantity = newQuantity
		orderBookEntry.Allocations = newAllocations
	}
}
