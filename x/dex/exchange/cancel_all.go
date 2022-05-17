package exchange

import (
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func CancelAll(
	transientOrders *dexcache.Orders,
	longBook []types.OrderBook,
	shortBook []types.OrderBook,
) ([]uint64, []uint64) {
	accountSet := map[string]uint64{}
	for _, cancelAll := range transientOrders.CancelAlls {
		accountSet[cancelAll.Creator] = 0
	}
	newTransientLimitBuys := []dexcache.LimitOrder{}
	for _, limitBuy := range transientOrders.LimitBuys {
		if _, ok := accountSet[limitBuy.Creator]; !ok {
			newTransientLimitBuys = append(newTransientLimitBuys, limitBuy)
		}
	}
	transientOrders.LimitBuys = newTransientLimitBuys
	newTransientLimitSells := []dexcache.LimitOrder{}
	for _, limitSell := range transientOrders.LimitSells {
		if _, ok := accountSet[limitSell.Creator]; !ok {
			newTransientLimitSells = append(newTransientLimitSells, limitSell)
		}
	}
	transientOrders.LimitSells = newTransientLimitSells
	newTransientMarketBuys := []dexcache.MarketOrder{}
	for _, marketBuy := range transientOrders.MarketBuys {
		if _, ok := accountSet[marketBuy.Creator]; !ok || marketBuy.IsLiquidation {
			newTransientMarketBuys = append(newTransientMarketBuys, marketBuy)
		}
	}
	transientOrders.MarketBuys = newTransientMarketBuys
	newTransientMarketSells := []dexcache.MarketOrder{}
	for _, marketSell := range transientOrders.MarketSells {
		if _, ok := accountSet[marketSell.Creator]; !ok || marketSell.IsLiquidation {
			newTransientMarketSells = append(newTransientMarketSells, marketSell)
		}
	}
	transientOrders.MarketSells = newTransientMarketSells

	dirtyLongIds := []uint64{}
	dirtyShortIds := []uint64{}
	for _, order := range longBook {
		if RemoveAllocations(order.GetEntry(), accountSet) {
			dirtyLongIds = append(dirtyLongIds, order.GetId())
		}
	}
	for _, order := range shortBook {
		if RemoveAllocations(order.GetEntry(), accountSet) {
			dirtyLongIds = append(dirtyShortIds, order.GetId())
		}
	}
	return dirtyLongIds, dirtyShortIds
}
