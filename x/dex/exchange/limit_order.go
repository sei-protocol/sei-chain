package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func MatchLimitOrders(
	ctx sdk.Context,
	orderbook *types.OrderBook,
) ExecutionOutcome {
	settlements := []*types.SettlementEntry{}
	totalExecuted, totalPrice := sdk.ZeroDec(), sdk.ZeroDec()
	longPtr, shortPtr := len(orderbook.Longs.Entries)-1, 0

	for longPtr >= 0 && shortPtr < len(orderbook.Shorts.Entries) && orderbook.Longs.Entries[longPtr].GetPrice().GTE(orderbook.Shorts.Entries[shortPtr].GetPrice()) {
		var executed sdk.Dec
		if orderbook.Longs.Entries[longPtr].GetEntry().Quantity.LT(orderbook.Shorts.Entries[shortPtr].GetEntry().Quantity) {
			executed = orderbook.Longs.Entries[longPtr].GetEntry().Quantity
		} else {
			executed = orderbook.Shorts.Entries[shortPtr].GetEntry().Quantity
		}
		totalExecuted = totalExecuted.Add(executed).Add(executed)
		totalPrice = totalPrice.Add(
			executed.Mul(
				orderbook.Longs.Entries[longPtr].GetPrice().Add(orderbook.Shorts.Entries[shortPtr].GetPrice()),
			),
		)

		orderbook.Longs.AddDirtyEntry(orderbook.Longs.Entries[longPtr])
		orderbook.Shorts.AddDirtyEntry(orderbook.Shorts.Entries[shortPtr])
		newSettlements := SettleFromBook(
			ctx,
			orderbook.Longs.Entries[longPtr],
			orderbook.Shorts.Entries[shortPtr],
			executed,
		)
		settlements = append(settlements, newSettlements...)

		if orderbook.Longs.Entries[longPtr].GetEntry().Quantity.IsZero() {
			longPtr--
		}
		if orderbook.Shorts.Entries[shortPtr].GetEntry().Quantity.IsZero() {
			shortPtr++
		}
	}
	return ExecutionOutcome{
		TotalNotional: totalPrice,
		TotalQuantity: totalExecuted,
		Settlements:   settlements,
	}
}

func addOrderToOrderBookEntry(
	order *types.Order,
	orderBookEntries *types.CachedSortedOrderBookEntries,
) {
	insertAt := -1
	newAllocation := &types.Allocation{
		OrderId:  order.Id,
		Quantity: order.Quantity,
		Account:  order.Account,
	}
	for i, ob := range orderBookEntries.Entries {
		if ob.GetPrice().Equal(order.Price) {
			orderBookEntries.AddDirtyEntry(ob)
			ob.GetEntry().Quantity = ob.GetEntry().Quantity.Add(order.Quantity)
			ob.GetEntry().Allocations = append(ob.GetEntry().Allocations, newAllocation)
			return
		}
		if order.Price.LT(ob.GetPrice()) {
			insertAt = i
			break
		}
	}
	var newOrder types.OrderBookEntry
	switch order.PositionDirection {
	case types.PositionDirection_LONG:
		newOrder = &types.LongBook{
			Price: order.Price,
			Entry: &types.OrderEntry{
				Price:       order.Price,
				Quantity:    order.Quantity,
				Allocations: []*types.Allocation{newAllocation},
				PriceDenom:  order.PriceDenom,
				AssetDenom:  order.AssetDenom,
			},
		}
	case types.PositionDirection_SHORT:
		newOrder = &types.ShortBook{
			Price: order.Price,
			Entry: &types.OrderEntry{
				Price:       order.Price,
				Quantity:    order.Quantity,
				Allocations: []*types.Allocation{newAllocation},
				PriceDenom:  order.PriceDenom,
				AssetDenom:  order.AssetDenom,
			},
		}
	}
	if insertAt == -1 {
		orderBookEntries.Entries = append(orderBookEntries.Entries, newOrder)
	} else {
		orderBookEntries.Entries = append(orderBookEntries.Entries, &types.LongBook{})
		copy(orderBookEntries.Entries[insertAt+1:], orderBookEntries.Entries[insertAt:])
		orderBookEntries.Entries[insertAt] = newOrder
	}
	orderBookEntries.AddDirtyEntry(newOrder)
}

func AddOutstandingLimitOrdersToOrderbook(
	orderbook *types.OrderBook,
	limitBuys []*types.Order,
	limitSells []*types.Order,
) {
	for _, order := range limitBuys {
		addOrderToOrderBookEntry(order, orderbook.Longs)
	}
	for _, order := range limitSells {
		addOrderToOrderBookEntry(order, orderbook.Shorts)
	}
}
