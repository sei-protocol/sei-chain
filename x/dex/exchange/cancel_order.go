package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func CancelOrders(
	ctx sdk.Context,
	cancels []dexcache.CancelOrder,
	book []types.OrderBook,
	direction types.PositionDirection,
	dirtyPrices *DirtyPrices,
) {
	for _, cancel := range cancels {
		for _, order := range book {
			if !cancel.Price.Equal(order.GetPrice()) {
				continue
			}
			if RemoveAllocations(order.GetEntry(), map[string]sdk.Dec{
				cancel.FormattedCreatorWithSuffix(): cancel.Quantity,
			}) {
				dirtyPrices.Add(order.GetPrice())
			}
		}
	}
}

func CancelForLiquidation(
	ctx sdk.Context,
	liquidationCancels []dexcache.CancellationFromLiquidation,
	book []types.OrderBook,
	dirtyPrices *DirtyPrices,
) {
	liquidatedAccountSet := utils.NewStringSet([]string{})
	for _, lc := range liquidationCancels {
		liquidatedAccountSet.Add(lc.Creator)
	}
	for _, order := range book {
		if RemoveEntireAllocations(order.GetEntry(), liquidatedAccountSet) {
			dirtyPrices.Add(order.GetPrice())
		}
	}
}
