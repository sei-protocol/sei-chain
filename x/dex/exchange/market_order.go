package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func MatchMarketOrders(
	ctx sdk.Context,
	marketOrders []dexcache.MarketOrder,
	orderBook []types.OrderBook,
	pair types.Pair,
	buy bool,
	dirtyOrderIds map[uint64]bool,
	settlements *[]*types.Settlement,
) (uint64, uint64) {
	var totalExecuted, totalPrice uint64 = 0, 0
	for idx, marketOrder := range marketOrders {
		for i := range orderBook {
			var existingOrder types.OrderBook
			if buy {
				existingOrder = orderBook[i]
			} else {
				existingOrder = orderBook[len(orderBook)-i-1]
			}
			if existingOrder.GetEntry().Quantity == 0 {
				continue
			}
			if (buy && marketOrder.WorstPrice < existingOrder.GetEntry().Price) ||
				(!buy && marketOrder.WorstPrice > existingOrder.GetEntry().Price) {
				break
			}
			var executed uint64
			if marketOrder.Quantity <= existingOrder.GetEntry().Quantity {
				executed = marketOrder.Quantity
			} else {
				executed = existingOrder.GetEntry().Quantity
			}
			marketOrder.Quantity -= executed
			totalExecuted += executed
			totalPrice += executed * (existingOrder.GetEntry().Price + marketOrder.WorstPrice) / 2
			dirtyOrderIds[existingOrder.GetId()] = true
			newSettlements := Settle(marketOrder.FormattedCreatorWithSuffix(), executed, existingOrder, buy, marketOrder.WorstPrice)
			*settlements = append(*settlements, newSettlements...)
			if marketOrder.Quantity == 0 {
				break
			}
		}
		marketOrders[idx].Quantity = marketOrder.Quantity
	}
	return totalPrice, totalExecuted
}
