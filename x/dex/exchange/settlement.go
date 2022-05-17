package exchange

import (
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type ToSettle struct {
	creator string
	amount  uint64
}

func Settle(
	taker string,
	quantityTaken uint64,
	order types.OrderBook,
	takerLong bool,
	worstPrice uint64,
) []*types.Settlement {
	settlements := []*types.Settlement{}
	order.GetEntry().Quantity -= quantityTaken
	newAllocations := RebalanceAllocations(order)
	newToSettle := []ToSettle{}
	nonZeroNewAllocations := []uint64{}
	nonZeroNewCreators := []string{}
	for i := 0; i < len(order.GetEntry().Allocation); i++ {
		creator := order.GetEntry().AllocationCreator[i]
		newToSettle = append(newToSettle, ToSettle{
			amount:  order.GetEntry().Allocation[i] - newAllocations[creator],
			creator: creator,
		})
		if newAllocations[creator] > 0 {
			nonZeroNewAllocations = append(nonZeroNewAllocations, newAllocations[creator])
			nonZeroNewCreators = append(nonZeroNewCreators, creator)
		}
	}
	order.GetEntry().Allocation = nonZeroNewAllocations
	order.GetEntry().AllocationCreator = nonZeroNewCreators
	avgPrice := (worstPrice + order.GetEntry().Price) / 2
	for _, toSettle := range newToSettle {
		settlements = append(settlements, types.NewSettlement(
			taker,
			takerLong,
			order.GetEntry().GetPriceDenom(),
			order.GetEntry().GetAssetDenom(),
			toSettle.amount,
			avgPrice,
			worstPrice,
		), types.NewSettlement(
			toSettle.creator,
			!takerLong,
			order.GetEntry().GetPriceDenom(),
			order.GetEntry().GetAssetDenom(),
			toSettle.amount,
			avgPrice,
			order.GetEntry().Price,
		))
	}
	return settlements
}

func SettleFromBook(
	longOrder types.OrderBook,
	shortOrder types.OrderBook,
	executedQuantity uint64,
) []*types.Settlement {
	settlements := []*types.Settlement{}
	longOrder.GetEntry().Quantity -= executedQuantity
	shortOrder.GetEntry().Quantity -= executedQuantity
	newLongAllocations := RebalanceAllocations(longOrder)
	newShortAllocations := RebalanceAllocations(shortOrder)
	newLongToSettle := []ToSettle{}
	newShortToSettle := []ToSettle{}
	nonZeroNewLongAllocations, nonZeroNewShortAllocations := []uint64{}, []uint64{}
	nonZeroNewLongCreators, nonZeroNewShortCreators := []string{}, []string{}
	for i := 0; i < len(longOrder.GetEntry().Allocation); i++ {
		creator := longOrder.GetEntry().AllocationCreator[i]
		newLongToSettle = append(newLongToSettle, ToSettle{amount: longOrder.GetEntry().Allocation[i] - newLongAllocations[creator], creator: creator})
		if newLongAllocations[creator] > 0 {
			nonZeroNewLongAllocations = append(nonZeroNewLongAllocations, newLongAllocations[creator])
			nonZeroNewLongCreators = append(nonZeroNewLongCreators, creator)
		}
	}
	longOrder.GetEntry().Allocation = nonZeroNewLongAllocations
	longOrder.GetEntry().AllocationCreator = nonZeroNewLongCreators
	for i := 0; i < len(shortOrder.GetEntry().Allocation); i++ {
		creator := shortOrder.GetEntry().AllocationCreator[i]
		newShortToSettle = append(newShortToSettle, ToSettle{amount: shortOrder.GetEntry().Allocation[i] - newShortAllocations[creator], creator: creator})
		if newShortAllocations[creator] > 0 {
			nonZeroNewShortAllocations = append(nonZeroNewShortAllocations, newShortAllocations[creator])
			nonZeroNewShortCreators = append(nonZeroNewShortCreators, creator)
		}
	}
	shortOrder.GetEntry().Allocation = nonZeroNewShortAllocations
	shortOrder.GetEntry().AllocationCreator = nonZeroNewShortCreators
	longPtr, shortPtr := 0, 0
	avgPrice := (longOrder.GetEntry().Price + shortOrder.GetEntry().Price) / 2
	for longPtr < len(newLongToSettle) && shortPtr < len(newShortToSettle) {
		longToSettle := newLongToSettle[longPtr]
		shortToSettle := newShortToSettle[shortPtr]
		var quantity uint64
		if longToSettle.amount < shortToSettle.amount {
			quantity = longToSettle.amount
		} else {
			quantity = shortToSettle.amount
		}
		settlements = append(settlements, types.NewSettlement(
			longToSettle.creator,
			true,
			longOrder.GetEntry().PriceDenom,
			longOrder.GetEntry().AssetDenom,
			quantity,
			avgPrice,
			longOrder.GetEntry().Price,
		), types.NewSettlement(
			shortToSettle.creator,
			false,
			shortOrder.GetEntry().PriceDenom,
			shortOrder.GetEntry().AssetDenom,
			quantity,
			avgPrice,
			shortOrder.GetEntry().Price,
		))
		newLongToSettle[longPtr] = ToSettle{creator: longToSettle.creator, amount: longToSettle.amount - quantity}
		newShortToSettle[shortPtr] = ToSettle{creator: shortToSettle.creator, amount: shortToSettle.amount - quantity}
		if newLongToSettle[longPtr].amount == 0 {
			longPtr++
		}
		if newShortToSettle[shortPtr].amount == 0 {
			shortPtr++
		}
	}
	return settlements
}
