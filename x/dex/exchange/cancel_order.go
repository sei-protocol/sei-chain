package exchange

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func CancelOrders(
	ctx sdk.Context, keeper *keeper.Keeper, contract types.ContractAddress, pair types.Pair,
	cancels []*types.Cancellation,
) {
	for _, cancel := range cancels {
		cancelOrder(ctx, keeper, cancel, contract, pair)
	}
}

func cancelOrder(ctx sdk.Context, keeper *keeper.Keeper, cancellation *types.Cancellation, contract types.ContractAddress, pair types.Pair) {
	getter, setter, deleter := keeper.GetLongOrderBookEntryByPrice, keeper.SetLongOrderBookEntry, keeper.RemoveLongBookByPrice
	if cancellation.PositionDirection == types.PositionDirection_SHORT {
		getter, setter, deleter = keeper.GetShortOrderBookEntryByPrice, keeper.SetShortOrderBookEntry, keeper.RemoveShortBookByPrice
	}
	entry, found := getter(ctx, string(contract), cancellation.Price, pair.PriceDenom, pair.AssetDenom)
	if !found {
		return
	}
	newEntry := *entry.GetOrderEntry()
	newAllocations := []*types.Allocation{}
	newQuantity := sdk.ZeroDec()
	for _, allocation := range newEntry.Allocations {
		if allocation.OrderId != cancellation.Id {
			newAllocations = append(newAllocations, allocation)
			newQuantity = newQuantity.Add(allocation.Quantity)
		}
	}
	numAllocationsRemoved := len(newEntry.Allocations) - len(newAllocations)
	if numAllocationsRemoved > 0 {
		err := keeper.DecreaseOrderCount(ctx, string(contract), pair.PriceDenom, pair.AssetDenom, cancellation.PositionDirection, entry.GetPrice(), uint64(numAllocationsRemoved))
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("error decreasing order count: %s", err))
		}
	}
	if newQuantity.IsZero() {
		deleter(ctx, string(contract), entry.GetPrice(), pair.PriceDenom, pair.AssetDenom)
		return
	}
	newEntry.Quantity = newQuantity
	newEntry.Allocations = newAllocations
	entry.SetEntry(&newEntry)
	setter(ctx, string(contract), entry)
}
