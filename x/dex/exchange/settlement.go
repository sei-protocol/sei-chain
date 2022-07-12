package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type ToSettle struct {
	orderID uint64
	amount  sdk.Dec
	account string
}

type AccountOrderID struct {
	Account string
	OrderID uint64
}

func Settle(
	takerOrder types.Order,
	quantityTaken sdk.Dec,
	order types.OrderBook,
	worstPrice sdk.Dec,
) ([]*types.SettlementEntry, []*types.SettlementEntry, []AccountOrderID) {
	// settlement of one liquidity taker's order is allocated to all liquidity
	// providers at the matched price level, propotional to the amount of liquidity
	// provided by each LP.
	takerSettlements := []*types.SettlementEntry{}
	makerSettlements := []*types.SettlementEntry{}
	order.GetEntry().Quantity = order.GetEntry().Quantity.Sub(quantityTaken)
	newAllocations := RebalanceAllocations(order)
	newToSettle := []ToSettle{}
	nonZeroNewAllocations := []*types.Allocation{}
	zeroAccountOrderIds := []AccountOrderID{}
	for _, allocation := range order.GetEntry().Allocations {
		newToSettle = append(newToSettle, ToSettle{
			amount:  allocation.Quantity.Sub(newAllocations[allocation.OrderId]),
			orderID: allocation.OrderId,
			account: allocation.Account,
		})
		if newAllocations[allocation.OrderId].IsPositive() {
			nonZeroNewAllocations = append(nonZeroNewAllocations, &types.Allocation{
				OrderId:  allocation.OrderId,
				Quantity: newAllocations[allocation.OrderId],
				Account:  allocation.Account,
			})
		} else {
			zeroAccountOrderIds = append(zeroAccountOrderIds, AccountOrderID{
				Account: allocation.Account,
				OrderID: allocation.OrderId,
			})
		}
	}
	order.GetEntry().Allocations = nonZeroNewAllocations
	for _, toSettle := range newToSettle {
		takerSettlements = append(takerSettlements, types.NewSettlementEntry(
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
		makerSettlements = append(makerSettlements, types.NewSettlementEntry(
			toSettle.orderID,
			toSettle.account,
			types.OppositePositionDirection[takerOrder.PositionDirection],
			order.GetEntry().GetPriceDenom(),
			order.GetEntry().GetAssetDenom(),
			toSettle.amount,
			order.GetEntry().Price,
			order.GetEntry().Price,
			types.OrderType_LIMIT,
		))
	}
	return takerSettlements, makerSettlements, zeroAccountOrderIds
}

func SettleFromBook(
	longOrder types.OrderBook,
	shortOrder types.OrderBook,
	executedQuantity sdk.Dec,
) ([]*types.SettlementEntry, []AccountOrderID) {
	// settlement from within the order book is also allocated to all liquidity
	// providers at the matched price level on both sides, propotional to the
	// amount of liquidity provided by each LP.
	settlements := []*types.SettlementEntry{}
	longOrder.GetEntry().Quantity = longOrder.GetEntry().Quantity.Sub(executedQuantity)
	shortOrder.GetEntry().Quantity = shortOrder.GetEntry().Quantity.Sub(executedQuantity)
	newLongAllocations := RebalanceAllocations(longOrder)
	newShortAllocations := RebalanceAllocations(shortOrder)
	newLongToSettle := []ToSettle{}
	newShortToSettle := []ToSettle{}
	nonZeroNewLongAllocations, nonZeroNewShortAllocations := []*types.Allocation{}, []*types.Allocation{}
	zeroAccountOrderIds := []AccountOrderID{}
	for _, allocation := range longOrder.GetEntry().Allocations {
		newLongToSettle = append(newLongToSettle, ToSettle{
			amount:  allocation.Quantity.Sub(newLongAllocations[allocation.OrderId]),
			account: allocation.Account,
			orderID: allocation.OrderId,
		})
		if newLongAllocations[allocation.OrderId].IsPositive() {
			nonZeroNewLongAllocations = append(nonZeroNewLongAllocations, &types.Allocation{
				OrderId:  allocation.OrderId,
				Quantity: newLongAllocations[allocation.OrderId],
				Account:  allocation.Account,
			})
		} else {
			zeroAccountOrderIds = append(zeroAccountOrderIds, AccountOrderID{
				Account: allocation.Account,
				OrderID: allocation.OrderId,
			})
		}
	}
	longOrder.GetEntry().Allocations = nonZeroNewLongAllocations
	for _, allocation := range shortOrder.GetEntry().Allocations {
		newShortToSettle = append(newShortToSettle, ToSettle{
			amount:  allocation.Quantity.Sub(newShortAllocations[allocation.OrderId]),
			account: allocation.Account,
			orderID: allocation.OrderId,
		})
		if newShortAllocations[allocation.OrderId].IsPositive() {
			nonZeroNewShortAllocations = append(nonZeroNewShortAllocations, &types.Allocation{
				OrderId:  allocation.OrderId,
				Quantity: newShortAllocations[allocation.OrderId],
				Account:  allocation.Account,
			})
		} else {
			zeroAccountOrderIds = append(zeroAccountOrderIds, AccountOrderID{
				Account: allocation.Account,
				OrderID: allocation.OrderId,
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
		settlements = append(settlements, types.NewSettlementEntry(
			longToSettle.orderID,
			longToSettle.account,
			types.PositionDirection_LONG,
			longOrder.GetEntry().PriceDenom,
			longOrder.GetEntry().AssetDenom,
			quantity,
			avgPrice,
			longOrder.GetPrice(),
			types.OrderType_LIMIT,
		), types.NewSettlementEntry(
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
	return settlements, zeroAccountOrderIds
}
