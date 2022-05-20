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
	longDirtyOrderIds map[uint64]bool,
	shortDirtyOrderIds map[uint64]bool,
	settlements *[]*types.Settlement,
) (uint64, uint64) {
	for _, order := range longOrders {
		addOrderToOrderBook(order, longBook, pair, longDirtyOrderIds)
	}
	for _, order := range shortOrders {
		addOrderToOrderBook(order, shortBook, pair, shortDirtyOrderIds)
	}
	var totalExecuted, totalPrice uint64 = 0, 0
	longPtr, shortPtr := len(*longBook)-1, 0

	for longPtr >= 0 && shortPtr < len(*shortBook) && (*longBook)[longPtr].GetEntry().Price >= (*shortBook)[shortPtr].GetEntry().Price {
		var executed uint64
		if (*longBook)[longPtr].GetEntry().Quantity < (*shortBook)[shortPtr].GetEntry().Quantity {
			executed = (*longBook)[longPtr].GetEntry().Quantity
		} else {
			executed = (*shortBook)[shortPtr].GetEntry().Quantity
		}
		totalExecuted += executed * 2
		totalPrice += executed * ((*longBook)[longPtr].GetEntry().GetPrice() + (*shortBook)[shortPtr].GetEntry().GetPrice())

		longDirtyOrderIds[(*longBook)[longPtr].GetId()] = true
		shortDirtyOrderIds[(*shortBook)[shortPtr].GetId()] = true
		*settlements = append(*settlements, SettleFromBook(
			(*longBook)[longPtr],
			(*shortBook)[shortPtr],
			executed,
		)...)

		if (*longBook)[longPtr].GetEntry().Quantity == 0 {
			longPtr -= 1
		}
		if (*shortBook)[shortPtr].GetEntry().Quantity == 0 {
			shortPtr += 1
		}
	}
	return totalPrice, totalExecuted
}

func addOrderToOrderBook(
	order dexcache.LimitOrder,
	orderBook *[]types.OrderBook,
	pair types.Pair,
	dirtyOrderIds map[uint64]bool,
) {
	isLong := order.Long
	insertAt := -1
	for i, ob := range *orderBook {
		if ob.GetEntry().Price == order.Price {
			dirtyOrderIds[ob.GetId()] = true
			ob.GetEntry().Quantity += order.Quantity
			existing := false
			for j, allocation := range ob.GetEntry().AllocationCreator {
				if allocation == order.FormattedCreatorWithSuffix() {
					existing = true
					ob.GetEntry().Allocation[j] += order.Quantity
					break
				}
			}
			if !existing {
				ob.GetEntry().AllocationCreator = append(ob.GetEntry().AllocationCreator, order.FormattedCreatorWithSuffix())
				ob.GetEntry().Allocation = append(ob.GetEntry().Allocation, order.Quantity)
			}
			return
		}
		if order.Price < ob.GetEntry().GetPrice() {
			insertAt = i
			break
		}
	}
	var newOrder types.OrderBook
	if isLong {
		newOrder = &types.LongBook{
			Id: order.Price,
			Entry: &types.OrderEntry{
				Price:             order.Price,
				Quantity:          order.Quantity,
				AllocationCreator: []string{order.FormattedCreatorWithSuffix()},
				Allocation:        []uint64{order.Quantity},
				PriceDenom:        pair.PriceDenom,
				AssetDenom:        pair.AssetDenom,
			},
		}
	} else {
		newOrder = &types.ShortBook{
			Id: order.Price,
			Entry: &types.OrderEntry{
				Price:             order.Price,
				Quantity:          order.Quantity,
				AllocationCreator: []string{order.FormattedCreatorWithSuffix()},
				Allocation:        []uint64{order.Quantity},
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
	dirtyOrderIds[order.Price] = true
	return
}
