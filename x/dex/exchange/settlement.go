package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	cache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// this function helps to settle market orders
func Settle(
	ctx sdk.Context,
	takerOrder *types.Order,
	quantityTaken sdk.Dec,
	orderbook *types.CachedSortedOrderBookEntries,
	worstPrice sdk.Dec,
	makerPrice sdk.Dec,
) ([]*types.SettlementEntry, []*types.SettlementEntry) {
	// settlement of one liquidity taker's order is allocated on a FIFO basis
	takerSettlements := []*types.SettlementEntry{}
	makerSettlements := []*types.SettlementEntry{}
	if quantityTaken.IsZero() {
		return takerSettlements, makerSettlements
	}
	newToSettle, _ := orderbook.SettleQuantity(ctx, quantityTaken)
	for _, toSettle := range newToSettle {
		takerSettlements = append(takerSettlements, types.NewSettlementEntry(
			ctx,
			takerOrder.Id,
			takerOrder.Account,
			takerOrder.PositionDirection,
			takerOrder.PriceDenom,
			takerOrder.AssetDenom,
			toSettle.Amount,
			worstPrice,
			worstPrice,
			takerOrder.OrderType,
		))
		makerSettlements = append(makerSettlements, types.NewSettlementEntry(
			ctx,
			toSettle.OrderID,
			toSettle.Account,
			types.OppositePositionDirection[takerOrder.PositionDirection],
			takerOrder.PriceDenom,
			takerOrder.AssetDenom,
			toSettle.Amount,
			makerPrice,
			makerPrice,
			types.OrderType_LIMIT,
		))
	}

	return takerSettlements, makerSettlements
}

// this function update the order data in the memState
// to be noted that the order status will only reflect for market orders that are settled
func UpdateOrderData(
	takerOrder *types.Order,
	quantityTaken sdk.Dec,
	blockOrders *cache.BlockOrders,
) {
	// update order data in the memstate
	orderStored := blockOrders.GetByID(takerOrder.Id)
	orderStored.Quantity = orderStored.Quantity.Sub(quantityTaken)
	if orderStored.OrderType == types.OrderType_FOKMARKET || orderStored.OrderType == types.OrderType_FOKMARKETBYVALUE || orderStored.Quantity.IsZero() {
		orderStored.Status = types.OrderStatus_FULFILLED
	}
	blockOrders.Add(orderStored)
}

func SettleFromBook(
	ctx sdk.Context,
	orderbook *types.OrderBook,
	executedQuantity sdk.Dec,
	longPrice sdk.Dec,
	shortPrice sdk.Dec,
) []*types.SettlementEntry {
	// settlement from within the order book is also allocated on a FIFO basis
	settlements := []*types.SettlementEntry{}
	if executedQuantity.IsZero() {
		return settlements
	}
	newLongToSettle, _ := orderbook.Longs.SettleQuantity(ctx, executedQuantity)
	newShortToSettle, _ := orderbook.Shorts.SettleQuantity(ctx, executedQuantity)
	avgPrice := longPrice.Add(shortPrice).Quo(sdk.NewDec(2))
	longPtr, shortPtr := 0, 0
	for longPtr < len(newLongToSettle) && shortPtr < len(newShortToSettle) {
		longToSettle := newLongToSettle[longPtr]
		shortToSettle := newShortToSettle[shortPtr]
		var quantity sdk.Dec
		if longToSettle.Amount.LT(shortToSettle.Amount) {
			quantity = longToSettle.Amount
		} else {
			quantity = shortToSettle.Amount
		}
		settlements = append(settlements, types.NewSettlementEntry(
			ctx,
			longToSettle.OrderID,
			longToSettle.Account,
			types.PositionDirection_LONG,
			orderbook.Pair.PriceDenom,
			orderbook.Pair.AssetDenom,
			quantity,
			avgPrice,
			longPrice,
			types.OrderType_LIMIT,
		), types.NewSettlementEntry(
			ctx,
			shortToSettle.OrderID,
			shortToSettle.Account,
			types.PositionDirection_SHORT,
			orderbook.Pair.PriceDenom,
			orderbook.Pair.AssetDenom,
			quantity,
			avgPrice,
			shortPrice,
			types.OrderType_LIMIT,
		))
		newLongToSettle[longPtr] = types.ToSettle{Account: longToSettle.Account, Amount: longToSettle.Amount.Sub(quantity), OrderID: longToSettle.OrderID}
		newShortToSettle[shortPtr] = types.ToSettle{Account: shortToSettle.Account, Amount: shortToSettle.Amount.Sub(quantity), OrderID: shortToSettle.OrderID}
		if newLongToSettle[longPtr].Amount.IsZero() {
			longPtr++
		}
		if newShortToSettle[shortPtr].Amount.IsZero() {
			shortPtr++
		}
	}
	return settlements
}
