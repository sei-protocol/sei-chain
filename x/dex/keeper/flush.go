package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) FlushDirtyLongBook(ctx sdk.Context, contractAddr string, order types.OrderBook) {
	if order.GetEntry().Quantity.IsZero() {
		k.RemoveLongBookByPrice(ctx, contractAddr, order.GetEntry().Price, order.GetEntry().PriceDenom, order.GetEntry().AssetDenom)
	} else {
		longOrder := order.(*types.LongBook)
		k.SetLongBook(ctx, contractAddr, *longOrder)
	}
}

func (k Keeper) FlushDirtyShortBook(ctx sdk.Context, contractAddr string, order types.OrderBook) {
	if order.GetEntry().Quantity.IsZero() {
		k.RemoveShortBookByPrice(ctx, contractAddr, order.GetEntry().Price, order.GetEntry().PriceDenom, order.GetEntry().AssetDenom)
	} else {
		shortOrder := order.(*types.ShortBook)
		k.SetShortBook(ctx, contractAddr, *shortOrder)
	}
}
