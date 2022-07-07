package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func CancelOrders(
	ctx sdk.Context,
	cancels []types.Cancellation,
	book []types.OrderBook,
	originalOrders map[uint64]types.Order,
	dirtyPrices *DirtyPrices,
) {
	for _, cancel := range cancels {
		for _, order := range book {
			originalOrder := originalOrders[cancel.Id]
			if !originalOrder.Price.Equal(order.GetPrice()) {
				continue
			}
			orderBookEntry := order.GetEntry()
			newAllocations := []*types.Allocation{}
			newQuantity := sdk.ZeroDec()
			for _, allocation := range orderBookEntry.Allocations {
				if allocation.OrderId != cancel.Id {
					newAllocations = append(newAllocations, allocation)
					newQuantity = newQuantity.Add(allocation.Quantity)
				} else {
					// `Add` is idempotent
					dirtyPrices.Add(order.GetPrice())
				}
			}
			orderBookEntry.Quantity = newQuantity
			orderBookEntry.Allocations = newAllocations
		}
	}
}
