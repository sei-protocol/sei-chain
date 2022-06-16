package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type ToSettle struct {
	creator string
	amount  sdk.Dec
}

func Settle(
	taker string,
	quantityTaken sdk.Dec,
	order types.OrderBook,
	takerDirection types.PositionDirection,
	worstPrice sdk.Dec,
) ([]*types.Settlement, []*types.Settlement) {
	takerSettlements := []*types.Settlement{}
	makerSettlements := []*types.Settlement{}
	order.GetEntry().Quantity = order.GetEntry().Quantity.Sub(quantityTaken)
	newAllocations := RebalanceAllocations(order)
	newToSettle := []ToSettle{}
	nonZeroNewAllocations := []sdk.Dec{}
	nonZeroNewCreators := []string{}
	for i := 0; i < len(order.GetEntry().Allocation); i++ {
		creator := order.GetEntry().AllocationCreator[i]
		newToSettle = append(newToSettle, ToSettle{
			amount:  order.GetEntry().Allocation[i].Sub(newAllocations[creator]),
			creator: creator,
		})
		if newAllocations[creator].IsPositive() {
			nonZeroNewAllocations = append(nonZeroNewAllocations, newAllocations[creator])
			nonZeroNewCreators = append(nonZeroNewCreators, creator)
		}
	}
	order.GetEntry().Allocation = nonZeroNewAllocations
	order.GetEntry().AllocationCreator = nonZeroNewCreators
	for _, toSettle := range newToSettle {
		takerSettlements = append(takerSettlements, types.NewSettlement(
			taker,
			takerDirection,
			order.GetEntry().GetPriceDenom(),
			order.GetEntry().GetAssetDenom(),
			toSettle.amount,
			worstPrice,
			worstPrice,
		))
		makerSettlements = append(makerSettlements, types.NewSettlement(
			toSettle.creator,
			types.OPPOSITE_POSITION_DIRECTION[takerDirection],
			order.GetEntry().GetPriceDenom(),
			order.GetEntry().GetAssetDenom(),
			toSettle.amount,
			order.GetEntry().Price,
			order.GetEntry().Price,
		))
	}
	return takerSettlements, makerSettlements
}

func SettleFromBook(
	longOrder types.OrderBook,
	shortOrder types.OrderBook,
	executedQuantity sdk.Dec,
) []*types.Settlement {
	settlements := []*types.Settlement{}
	longOrder.GetEntry().Quantity = longOrder.GetEntry().Quantity.Sub(executedQuantity)
	shortOrder.GetEntry().Quantity = shortOrder.GetEntry().Quantity.Sub(executedQuantity)
	newLongAllocations := RebalanceAllocations(longOrder)
	newShortAllocations := RebalanceAllocations(shortOrder)
	newLongToSettle := []ToSettle{}
	newShortToSettle := []ToSettle{}
	nonZeroNewLongAllocations, nonZeroNewShortAllocations := []sdk.Dec{}, []sdk.Dec{}
	nonZeroNewLongCreators, nonZeroNewShortCreators := []string{}, []string{}
	for i := 0; i < len(longOrder.GetEntry().Allocation); i++ {
		creator := longOrder.GetEntry().AllocationCreator[i]
		newLongToSettle = append(newLongToSettle, ToSettle{
			amount:  longOrder.GetEntry().Allocation[i].Sub(newLongAllocations[creator]),
			creator: creator,
		})
		if newLongAllocations[creator].IsPositive() {
			nonZeroNewLongAllocations = append(nonZeroNewLongAllocations, newLongAllocations[creator])
			nonZeroNewLongCreators = append(nonZeroNewLongCreators, creator)
		}
	}
	longOrder.GetEntry().Allocation = nonZeroNewLongAllocations
	longOrder.GetEntry().AllocationCreator = nonZeroNewLongCreators
	for i := 0; i < len(shortOrder.GetEntry().Allocation); i++ {
		creator := shortOrder.GetEntry().AllocationCreator[i]
		newShortToSettle = append(newShortToSettle, ToSettle{
			amount:  shortOrder.GetEntry().Allocation[i].Sub(newShortAllocations[creator]),
			creator: creator,
		})
		if newShortAllocations[creator].IsPositive() {
			nonZeroNewShortAllocations = append(nonZeroNewShortAllocations, newShortAllocations[creator])
			nonZeroNewShortCreators = append(nonZeroNewShortCreators, creator)
		}
	}
	shortOrder.GetEntry().Allocation = nonZeroNewShortAllocations
	shortOrder.GetEntry().AllocationCreator = nonZeroNewShortCreators
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
		settlements = append(settlements, types.NewSettlement(
			longToSettle.creator,
			types.PositionDirection_LONG,
			longOrder.GetEntry().PriceDenom,
			longOrder.GetEntry().AssetDenom,
			quantity,
			avgPrice,
			longOrder.GetPrice(),
		), types.NewSettlement(
			shortToSettle.creator,
			types.PositionDirection_SHORT,
			shortOrder.GetEntry().PriceDenom,
			shortOrder.GetEntry().AssetDenom,
			quantity,
			avgPrice,
			shortOrder.GetPrice(),
		))
		newLongToSettle[longPtr] = ToSettle{creator: longToSettle.creator, amount: longToSettle.amount.Sub(quantity)}
		newShortToSettle[shortPtr] = ToSettle{creator: shortToSettle.creator, amount: shortToSettle.amount.Sub(quantity)}
		if newLongToSettle[longPtr].amount.IsZero() {
			longPtr++
		}
		if newShortToSettle[shortPtr].amount.IsZero() {
			shortPtr++
		}
	}
	return settlements
}
