package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	cache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types/wasm"
)

type ToSettle struct {
	orderID uint64
	amount  sdk.Dec
	account string
}

// this function helps to settle market orders
func Settle(
	ctx sdk.Context,
	takerOrder *types.Order,
	quantityTaken sdk.Dec,
	order types.OrderBookEntry,
	worstPrice sdk.Dec,
	blockOrders *cache.BlockOrders,
) ([]*types.SettlementEntry, []*types.SettlementEntry) {
	// settlement of one liquidity taker's order is allocated to all liquidity
	// providers at the matched price level, propotional to the amount of liquidity
	// provided by each LP.
	takerSettlements := []*types.SettlementEntry{}
	makerSettlements := []*types.SettlementEntry{}
	if quantityTaken.IsZero() {
		return takerSettlements, makerSettlements
	}
	order.GetEntry().Quantity = order.GetEntry().Quantity.Sub(quantityTaken)
	settledQuantity := sdk.ZeroDec()
	newToSettle := []ToSettle{}
	nonZeroNewAllocations := []*types.Allocation{}
	for _, allocation := range order.GetEntry().Allocations {
		if allocation.Quantity.IsZero() {
			continue
		}
		if settledQuantity.LT(quantityTaken) {
			diff := quantityTaken.Sub(settledQuantity)
			if allocation.Quantity.GT(diff) {
				newToSettle = append(newToSettle, ToSettle{
					amount:  diff,
					orderID: allocation.OrderId,
					account: allocation.Account,
				})
				nonZeroNewAllocations = append(nonZeroNewAllocations, &types.Allocation{
					OrderId:  allocation.OrderId,
					Quantity: allocation.Quantity.Sub(diff),
					Account:  allocation.Account,
				})
				settledQuantity = quantityTaken
			} else {
				newToSettle = append(newToSettle, ToSettle{
					amount:  allocation.Quantity,
					orderID: allocation.OrderId,
					account: allocation.Account,
				})
				settledQuantity = settledQuantity.Add(allocation.Quantity)
			}
		} else {
			nonZeroNewAllocations = append(nonZeroNewAllocations, &types.Allocation{
				OrderId:  allocation.OrderId,
				Quantity: allocation.Quantity,
				Account:  allocation.Account,
			})
		}
	}
	order.GetEntry().Allocations = nonZeroNewAllocations
	for _, toSettle := range newToSettle {
		takerSettlements = append(takerSettlements, wasm.NewSettlementEntry(
			ctx,
			takerOrder.Id,
			takerOrder.Account,
			takerOrder.PositionDirection,
			order.GetEntry().GetPriceDenom(),
			order.GetEntry().GetAssetDenom(),
			toSettle.amount,
			worstPrice,
			worstPrice,
			takerOrder.OrderType,
		))
		makerSettlements = append(makerSettlements, wasm.NewSettlementEntry(
			ctx,
			toSettle.orderID,
			toSettle.account,
			utils.OppositePositionDirection[takerOrder.PositionDirection],
			order.GetEntry().GetPriceDenom(),
			order.GetEntry().GetAssetDenom(),
			toSettle.amount,
			order.GetEntry().Price,
			order.GetEntry().Price,
			types.OrderType_LIMIT,
		))
	}

	// update the status of order in the memState
	UpdateOrderData(takerOrder, quantityTaken, blockOrders)

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
	longOrder types.OrderBookEntry,
	shortOrder types.OrderBookEntry,
	executedQuantity sdk.Dec,
) []*types.SettlementEntry {
	// settlement from within the order book is also allocated to all liquidity
	// providers at the matched price level on both sides, propotional to the
	// amount of liquidity provided by each LP.
	settlements := []*types.SettlementEntry{}
	if executedQuantity.IsZero() {
		return settlements
	}
	longOrder.GetEntry().Quantity = longOrder.GetEntry().Quantity.Sub(executedQuantity)
	shortOrder.GetEntry().Quantity = shortOrder.GetEntry().Quantity.Sub(executedQuantity)
	newLongToSettle := []ToSettle{}
	newShortToSettle := []ToSettle{}
	nonZeroNewLongAllocations, nonZeroNewShortAllocations := []*types.Allocation{}, []*types.Allocation{}
	longSettledQuantity, shortSettledQuantity := sdk.ZeroDec(), sdk.ZeroDec()
	for _, allocation := range longOrder.GetEntry().Allocations {
		if allocation.Quantity.IsZero() {
			continue
		}
		if longSettledQuantity.LT(executedQuantity) {
			diff := executedQuantity.Sub(longSettledQuantity)
			if allocation.Quantity.GT(diff) {
				newLongToSettle = append(newLongToSettle, ToSettle{
					amount:  diff,
					orderID: allocation.OrderId,
					account: allocation.Account,
				})
				nonZeroNewLongAllocations = append(nonZeroNewLongAllocations, &types.Allocation{
					OrderId:  allocation.OrderId,
					Quantity: allocation.Quantity.Sub(diff),
					Account:  allocation.Account,
				})
				longSettledQuantity = executedQuantity
			} else {
				newLongToSettle = append(newLongToSettle, ToSettle{
					amount:  allocation.Quantity,
					orderID: allocation.OrderId,
					account: allocation.Account,
				})
				longSettledQuantity = longSettledQuantity.Add(allocation.Quantity)
			}
		} else {
			nonZeroNewLongAllocations = append(nonZeroNewLongAllocations, &types.Allocation{
				OrderId:  allocation.OrderId,
				Quantity: allocation.Quantity,
				Account:  allocation.Account,
			})
		}
	}
	longOrder.GetEntry().Allocations = nonZeroNewLongAllocations
	for _, allocation := range shortOrder.GetEntry().Allocations {
		if allocation.Quantity.IsZero() {
			continue
		}
		if shortSettledQuantity.LT(executedQuantity) {
			diff := executedQuantity.Sub(shortSettledQuantity)
			if allocation.Quantity.GT(diff) {
				newShortToSettle = append(newShortToSettle, ToSettle{
					amount:  diff,
					orderID: allocation.OrderId,
					account: allocation.Account,
				})
				nonZeroNewShortAllocations = append(nonZeroNewShortAllocations, &types.Allocation{
					OrderId:  allocation.OrderId,
					Quantity: allocation.Quantity.Sub(diff),
					Account:  allocation.Account,
				})
				shortSettledQuantity = executedQuantity
			} else {
				newShortToSettle = append(newShortToSettle, ToSettle{
					amount:  allocation.Quantity,
					orderID: allocation.OrderId,
					account: allocation.Account,
				})
				shortSettledQuantity = shortSettledQuantity.Add(allocation.Quantity)
			}
		} else {
			nonZeroNewShortAllocations = append(nonZeroNewShortAllocations, &types.Allocation{
				OrderId:  allocation.OrderId,
				Quantity: allocation.Quantity,
				Account:  allocation.Account,
			})
		}
	}
	shortOrder.GetEntry().Allocations = nonZeroNewShortAllocations
	longPtr, shortPtr := 0, 0
	avgPrice := longOrder.GetPrice().Add(shortOrder.GetPrice()).Quo(sdk.NewDec(2))
	for longPtr < len(newLongToSettle) && shortPtr < len(newShortToSettle) {
		longToSettle := newLongToSettle[longPtr]
		shortToSettle := newShortToSettle[shortPtr]
		var quantity sdk.Dec
		if longToSettle.amount.LT(shortToSettle.amount) {
			quantity = longToSettle.amount
		} else {
			quantity = shortToSettle.amount
		}
		settlements = append(settlements, wasm.NewSettlementEntry(
			ctx,
			longToSettle.orderID,
			longToSettle.account,
			types.PositionDirection_LONG,
			longOrder.GetEntry().PriceDenom,
			longOrder.GetEntry().AssetDenom,
			quantity,
			avgPrice,
			longOrder.GetPrice(),
			types.OrderType_LIMIT,
		), wasm.NewSettlementEntry(
			ctx,
			shortToSettle.orderID,
			shortToSettle.account,
			types.PositionDirection_SHORT,
			shortOrder.GetEntry().PriceDenom,
			shortOrder.GetEntry().AssetDenom,
			quantity,
			avgPrice,
			shortOrder.GetPrice(),
			types.OrderType_LIMIT,
		))
		newLongToSettle[longPtr] = ToSettle{account: longToSettle.account, amount: longToSettle.amount.Sub(quantity), orderID: longToSettle.orderID}
		newShortToSettle[shortPtr] = ToSettle{account: shortToSettle.account, amount: shortToSettle.amount.Sub(quantity), orderID: shortToSettle.orderID}
		if newLongToSettle[longPtr].amount.IsZero() {
			longPtr++
		}
		if newShortToSettle[shortPtr].amount.IsZero() {
			shortPtr++
		}
	}
	return settlements
}
