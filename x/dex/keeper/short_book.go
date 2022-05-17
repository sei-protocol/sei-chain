package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// SetShortBook set a specific shortBook in the store
func (k Keeper) SetShortBook(ctx sdk.Context, contractAddr string, shortBook types.ShortBook) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.OrderBookPrefix(false, contractAddr, shortBook.Entry.PriceDenom, shortBook.Entry.AssetDenom))
	b := k.cdc.MustMarshal(&shortBook)
	store.Set(GetKeyForShortBook(shortBook), b)
}

func (k Keeper) GetShortBookByPrice(ctx sdk.Context, contractAddr string, price uint64, priceDenom string, assetDenom string) (val types.ShortBook, found bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.OrderBookPrefix(false, contractAddr, priceDenom, assetDenom))
	b := store.Get(GetKeyForPrice(price))
	if b == nil {
		return val, false
	}
	k.cdc.MustUnmarshal(b, &val)
	return val, true
}

func (k Keeper) RemoveShortBookByPrice(ctx sdk.Context, contractAddr string, price uint64, priceDenom string, assetDenom string) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.OrderBookPrefix(false, contractAddr, priceDenom, assetDenom))
	store.Delete(GetKeyForPrice(price))
}

// GetAllShortBook returns all shortBook
func (k Keeper) GetAllShortBook(ctx sdk.Context, contractAddr string) (list []types.ShortBook) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.ContractKeyPrefix(types.ShortBookKey, contractAddr))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.ShortBook
		k.cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, val)
	}

	return
}

func (k Keeper) GetAllShortBookForPair(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string) (list []types.OrderBook) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.OrderBookPrefix(false, contractAddr, priceDenom, assetDenom))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.ShortBook
		k.cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, &val)
	}

	return
}

func GetKeyForShortBook(shortBook types.ShortBook) []byte {
	return GetKeyForPrice(shortBook.Entry.Price)
}
