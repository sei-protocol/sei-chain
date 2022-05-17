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
	long bool,
	dirtyOrderIds map[uint64]bool,
) {
	priceToCreatorsToQuantities := map[uint64]map[string]uint64{}
	for _, cancel := range cancels {
		if _, ok := priceToCreatorsToQuantities[uint64(cancel.Price)]; !ok {
			priceToCreatorsToQuantities[uint64(cancel.Price)] = map[string]uint64{}
		}
		creatorsToQuantities := priceToCreatorsToQuantities[uint64(cancel.Price)]
		if _, ok := creatorsToQuantities[cancel.FormattedCreatorWithSuffix()]; !ok {
			creatorsToQuantities[cancel.FormattedCreatorWithSuffix()] = 0
		}
		creatorsToQuantities[cancel.FormattedCreatorWithSuffix()] += uint64(cancel.Quantity)
	}
	for _, order := range book {
		var creatorsToQuantities map[string]uint64
		if val, ok := priceToCreatorsToQuantities[uint64(order.GetEntry().Price)]; !ok {
			creatorsToQuantities = map[string]uint64{}
		} else {
			creatorsToQuantities = val
		}
		if RemoveAllocations(order.GetEntry(), creatorsToQuantities) {
			dirtyOrderIds[order.GetId()] = true
		}
	}
}

func CancelForLiquidation(
	ctx sdk.Context,
	liquidationCancels []dexcache.CancellationFromLiquidation,
	book []types.OrderBook,
	dirtyOrderIds map[uint64]bool,
) {
	liquidatedAccountSet := utils.NewStringSet([]string{})
	for _, lc := range liquidationCancels {
		liquidatedAccountSet.Add(lc.Creator)
	}
	for _, order := range book {
		if RemoveEntireAllocations(order.GetEntry(), liquidatedAccountSet) {
			dirtyOrderIds[order.GetId()] = true
		}
	}
}
