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
	direction types.PositionDirection,
	dirtyPrices *DirtyPrices,
	settlements *[]*types.Settlement,
) (sdk.Dec, sdk.Dec) {
	var totalExecuted, totalPrice sdk.Dec = sdk.ZeroDec(), sdk.ZeroDec()
	for idx, marketOrder := range marketOrders {
		for i := range orderBook {
			var existingOrder types.OrderBook
			if direction == types.PositionDirection_LONG {
				existingOrder = orderBook[i]
			} else {
				existingOrder = orderBook[len(orderBook)-i-1]
			}
			if existingOrder.GetEntry().Quantity.IsZero() {
				continue
			}
			if (direction == types.PositionDirection_LONG && marketOrder.WorstPrice.LT(existingOrder.GetPrice())) ||
				(direction == types.PositionDirection_SHORT && marketOrder.WorstPrice.GT(existingOrder.GetPrice())) {
				break
			}
			var executed sdk.Dec
			if marketOrder.Quantity.LTE(existingOrder.GetEntry().Quantity) {
				executed = marketOrder.Quantity
			} else {
				executed = existingOrder.GetEntry().Quantity
			}
			marketOrder.Quantity = marketOrder.Quantity.Sub(executed)
			totalExecuted = totalExecuted.Add(executed)
			totalPrice = totalPrice.Add(
				executed.Mul(existingOrder.GetPrice().Add(marketOrder.WorstPrice)).Quo(sdk.NewDec(2)),
			)
			dirtyPrices.Add(existingOrder.GetPrice())
			newSettlements := Settle(marketOrder.FormattedCreatorWithSuffix(), executed, existingOrder, direction, marketOrder.WorstPrice)
			*settlements = append(*settlements, newSettlements...)
			if marketOrder.Quantity.IsZero() {
				break
			}
		}
		marketOrders[idx].Quantity = marketOrder.Quantity
	}
	return totalPrice, totalExecuted
}
