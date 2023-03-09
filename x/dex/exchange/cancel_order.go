package exchange

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func CancelOrders(
	cancels []*types.Cancellation,
	orderbook *types.OrderBook,
) {
	fmt.Printf("DEBUGDEX CancelOrders - numCancels %d\n", len(cancels))
	for _, cancel := range cancels {
		if cancel.PositionDirection == types.PositionDirection_LONG {
			cancelOrder(cancel, orderbook.Longs)
		} else {
			cancelOrder(cancel, orderbook.Shorts)
		}
	}
}

func cancelOrder(cancellation *types.Cancellation, orderBookEntries *types.CachedSortedOrderBookEntries) {
	for _, order := range orderBookEntries.Entries {
		if !cancellation.Price.Equal(order.GetPrice()) {
			continue
		}
		orderBookEntry := order.GetEntry()
		newAllocations := []*types.Allocation{}
		newQuantity := sdk.ZeroDec()
		for _, allocation := range orderBookEntry.Allocations {
			if allocation.OrderId != cancellation.Id {
				newAllocations = append(newAllocations, allocation)
				newQuantity = newQuantity.Add(allocation.Quantity)
			} else {
				// `Add` is idempotent
				orderBookEntries.AddDirtyEntry(order)
			}
		}
		orderBookEntry.Quantity = newQuantity
		orderBookEntry.Allocations = newAllocations
		return
	}
}
