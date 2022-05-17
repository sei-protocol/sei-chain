package keeper

import (
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) FlushDirtyLongBook(ctx sdk.Context, contractAddr string, order types.OrderBook) {
	if order.GetEntry().Quantity == 0 {
		k.RemoveLongBookByPrice(ctx, contractAddr, order.GetEntry().Price, order.GetEntry().PriceDenom, order.GetEntry().AssetDenom)
	} else {
		longOrder := order.(*types.LongBook)
		k.SetLongBook(ctx, contractAddr, *longOrder)
	}
}

func (k Keeper) FlushDirtyShortBook(ctx sdk.Context, contractAddr string, order types.OrderBook) {
	if order.GetEntry().Quantity == 0 {
		k.RemoveShortBookByPrice(ctx, contractAddr, order.GetEntry().Price, order.GetEntry().PriceDenom, order.GetEntry().AssetDenom)
	} else {
		shortOrder := order.(*types.ShortBook)
		k.SetShortBook(ctx, contractAddr, *shortOrder)
	}
}

type DirtyIds struct {
	ids map[uint64]bool
}

func NewDirtyIds() DirtyIds {
	return DirtyIds{
		ids: map[uint64]bool{},
	}
}

func (d *DirtyIds) AddId(id uint64) {
	d.ids[id] = true
}

func (d *DirtyIds) GetIds() []uint64 {
	result := []uint64{}
	for id := range d.ids {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i] < result[j]
	})
	return result
}
