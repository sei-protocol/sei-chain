package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func MatchLimitOrders(
	ctx sdk.Context,
	longOrders []dexcache.LimitOrder,
	shortOrders []dexcache.LimitOrder,
	longBook *[]types.OrderBook,
	shortBook *[]types.OrderBook,
	pair types.Pair,
	longDirtyPrices *DirtyPrices,
	shortDirtyPrices *DirtyPrices,
	settlements *[]*types.Settlement,
) (sdk.Dec, sdk.Dec) {
	for _, order := range longOrders {
		addOrderToOrderBook(order, longBook, pair, longDirtyPrices)
	}
	for _, order := range shortOrders {
		addOrderToOrderBook(order, shortBook, pair, shortDirtyPrices)
	}
	var totalExecuted, totalPrice sdk.Dec = sdk.ZeroDec(), sdk.ZeroDec()
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
		*settlements = append(*settlements, SettleFromBook(
			(*longBook)[longPtr],
			(*shortBook)[shortPtr],
			executed,
		)...)

		if (*longBook)[longPtr].GetEntry().Quantity.Equal(sdk.ZeroDec()) {
			longPtr -= 1
		}
		if (*shortBook)[shortPtr].GetEntry().Quantity.Equal(sdk.ZeroDec()) {
			shortPtr += 1
		}
	}
	return totalPrice, totalExecuted
}

func addOrderToOrderBook(
	order dexcache.LimitOrder,
	orderBook *[]types.OrderBook,
	pair types.Pair,
	dirtyPrices *DirtyPrices,
) {
	insertAt := -1
	for i, ob := range *orderBook {
		if ob.GetPrice().Equal(order.Price) {
			dirtyPrices.Add(ob.GetPrice())
			ob.GetEntry().Quantity = ob.GetEntry().Quantity.Add(order.Quantity)
			existing := false
			for j, allocation := range ob.GetEntry().AllocationCreator {
				if allocation == order.FormattedCreatorWithSuffix() {
					existing = true
					ob.GetEntry().Allocation[j] = ob.GetEntry().Allocation[j].Add(order.Quantity)
					break
				}
			}
			if !existing {
				ob.GetEntry().AllocationCreator = append(ob.GetEntry().AllocationCreator, order.FormattedCreatorWithSuffix())
				ob.GetEntry().Allocation = append(ob.GetEntry().Allocation, order.Quantity)
			}
			return
		}
		if order.Price.LT(ob.GetPrice()) {
			insertAt = i
			break
		}
	}
	var newOrder types.OrderBook
	switch order.Direction {
	case types.PositionDirection_LONG:
		newOrder = &types.LongBook{
			Price: order.Price,
			Entry: &types.OrderEntry{
				Price:             order.Price,
				Quantity:          order.Quantity,
				AllocationCreator: []string{order.FormattedCreatorWithSuffix()},
				Allocation:        []sdk.Dec{order.Quantity},
				PriceDenom:        pair.PriceDenom,
				AssetDenom:        pair.AssetDenom,
			},
		}
	case types.PositionDirection_SHORT:
		newOrder = &types.ShortBook{
			Price: order.Price,
			Entry: &types.OrderEntry{
				Price:             order.Price,
				Quantity:          order.Quantity,
				AllocationCreator: []string{order.FormattedCreatorWithSuffix()},
				Allocation:        []sdk.Dec{order.Quantity},
				PriceDenom:        pair.PriceDenom,
				AssetDenom:        pair.AssetDenom,
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
	return
}
