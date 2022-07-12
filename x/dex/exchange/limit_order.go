package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func MatchLimitOrders(
	ctx sdk.Context,
	longOrders []types.Order,
	shortOrders []types.Order,
	longBook *[]types.OrderBook,
	shortBook *[]types.OrderBook,
	pair types.Pair,
	longDirtyPrices *DirtyPrices,
	shortDirtyPrices *DirtyPrices,
	settlements *[]*types.SettlementEntry,
	zeroOrders *[]AccountOrderID,
) (sdk.Dec, sdk.Dec) {
	for _, order := range longOrders {
		addOrderToOrderBook(order, longBook, pair, longDirtyPrices)
	}
	for _, order := range shortOrders {
		addOrderToOrderBook(order, shortBook, pair, shortDirtyPrices)
	}
	totalExecuted, totalPrice := sdk.ZeroDec(), sdk.ZeroDec()
	longPtr, shortPtr := len(*longBook)-1, 0

	for longPtr >= 0 && shortPtr < len(*shortBook) && (*longBook)[longPtr].GetPrice().GTE((*shortBook)[shortPtr].GetPrice()) {
		var executed sdk.Dec
		if (*longBook)[longPtr].GetEntry().Quantity.LT((*shortBook)[shortPtr].GetEntry().Quantity) {
			executed = (*longBook)[longPtr].GetEntry().Quantity
		} else {
			executed = (*shortBook)[shortPtr].GetEntry().Quantity
		}
		totalExecuted = totalExecuted.Add(executed).Add(executed)
		totalPrice = totalPrice.Add(
			executed.Mul(
				(*longBook)[longPtr].GetPrice().Add((*shortBook)[shortPtr].GetPrice()),
			),
		)

		longDirtyPrices.Add((*longBook)[longPtr].GetPrice())
		shortDirtyPrices.Add((*shortBook)[shortPtr].GetPrice())
		newSettlements, zeroAccountOrders := SettleFromBook(
			(*longBook)[longPtr],
			(*shortBook)[shortPtr],
			executed,
		)
		*settlements = append(*settlements, newSettlements...)
		*zeroOrders = append(*zeroOrders, zeroAccountOrders...)

		if (*longBook)[longPtr].GetEntry().Quantity.IsZero() {
			longPtr--
		}
		if (*shortBook)[shortPtr].GetEntry().Quantity.IsZero() {
			shortPtr++
		}
	}
	return totalPrice, totalExecuted
}

func addOrderToOrderBook(
	order types.Order,
	orderBook *[]types.OrderBook,
	pair types.Pair,
	dirtyPrices *DirtyPrices,
) {
	insertAt := -1
	newAllocation := &types.Allocation{
		OrderId:  order.Id,
		Quantity: order.Quantity,
		Account:  order.Account,
	}
	for i, ob := range *orderBook {
		if ob.GetPrice().Equal(order.Price) {
			dirtyPrices.Add(ob.GetPrice())
			ob.GetEntry().Quantity = ob.GetEntry().Quantity.Add(order.Quantity)
			ob.GetEntry().Allocations = append(ob.GetEntry().Allocations, newAllocation)
			return
		}
		if order.Price.LT(ob.GetPrice()) {
			insertAt = i
			break
		}
	}
	var newOrder types.OrderBook
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
		*orderBook = append(*orderBook, newOrder)
	} else {
		*orderBook = append(*orderBook, &types.LongBook{})
		copy((*orderBook)[insertAt+1:], (*orderBook)[insertAt:])
		(*orderBook)[insertAt] = newOrder
	}
	dirtyPrices.Add(order.Price)
}
