package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// SetShortBook set a specific shortBook in the store
func (k Keeper) SetShortBook(ctx sdk.Context, contractAddr string, shortBook types.ShortBook) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.OrderBookPrefix(false, contractAddr, shortBook.Entry.PriceDenom, shortBook.Entry.AssetDenom))
	b := k.Cdc.MustMarshal(&shortBook)
	store.Set(GetKeyForShortBook(shortBook), b)
}

func (k Keeper) GetShortBookByPrice(ctx sdk.Context, contractAddr string, price sdk.Dec, priceDenom string, assetDenom string) (val types.ShortBook, found bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.OrderBookPrefix(false, contractAddr, priceDenom, assetDenom))
	b := store.Get(GetKeyForPrice(price))
	if b == nil {
		return val, false
	}
	k.Cdc.MustUnmarshal(b, &val)
	return val, true
}

func (k Keeper) RemoveShortBookByPrice(ctx sdk.Context, contractAddr string, price sdk.Dec, priceDenom string, assetDenom string) {
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
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
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
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, &val)
	}

	return
}

func (k Keeper) GetAllShortBookForPairPaginated(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, page *query.PageRequest) (list []types.ShortBook, pageRes *query.PageResponse, err error) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.OrderBookPrefix(false, contractAddr, priceDenom, assetDenom))

	pageRes, err = query.Paginate(store, page, func(key []byte, value []byte) error {
		var shortBook types.ShortBook
		if err := k.Cdc.Unmarshal(value, &shortBook); err != nil {
			return err
		}

		list = append(list, shortBook)
		return nil
	})

	return
}

func GetKeyForShortBook(shortBook types.ShortBook) []byte {
	return GetKeyForPrice(shortBook.Entry.Price)
}
