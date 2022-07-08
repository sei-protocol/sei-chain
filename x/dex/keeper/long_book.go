package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// SetLongBook set a specific longBook in the store
func (k Keeper) SetLongBook(ctx sdk.Context, contractAddr string, longBook types.LongBook) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.OrderBookPrefix(
			true, contractAddr, longBook.Entry.PriceDenom, longBook.Entry.AssetDenom,
		),
	)
	b := k.Cdc.MustMarshal(&longBook)
	store.Set(GetKeyForLongBook(longBook), b)
}

func (k Keeper) GetLongBookByPrice(ctx sdk.Context, contractAddr string, price sdk.Dec, priceDenom string, assetDenom string) (val types.LongBook, found bool) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.OrderBookPrefix(
			true, contractAddr, priceDenom, assetDenom,
		),
	)
	b := store.Get(GetKeyForPrice(price))
	if b == nil {
		return val, false
	}
	k.Cdc.MustUnmarshal(b, &val)
	return val, true
}

func (k Keeper) RemoveLongBookByPrice(ctx sdk.Context, contractAddr string, price sdk.Dec, priceDenom string, assetDenom string) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.OrderBookPrefix(
			true, contractAddr, priceDenom, assetDenom,
		),
	)
	store.Delete(GetKeyForPrice(price))
}

// GetAllLongBook returns all longBook
func (k Keeper) GetAllLongBook(ctx sdk.Context, contractAddr string) (list []types.LongBook) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.ContractKeyPrefix(types.LongBookKey, contractAddr))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.LongBook
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, val)
	}

	return
}

func (k Keeper) GetAllLongBookForPair(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string) (list []types.OrderBook) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.OrderBookPrefix(true, contractAddr, priceDenom, assetDenom))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.LongBook
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, &val)
	}

	return
}

func (k Keeper) GetAllLongBookForPairPaginated(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, page *query.PageRequest) (list []types.LongBook, pageRes *query.PageResponse, err error) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.OrderBookPrefix(true, contractAddr, priceDenom, assetDenom))

	pageRes, err = query.Paginate(store, page, func(key []byte, value []byte) error {
		var longBook types.LongBook
		if err := k.Cdc.Unmarshal(value, &longBook); err != nil {
			return err
		}

		list = append(list, longBook)
		return nil
	})

	return
}

func GetKeyForLongBook(longBook types.LongBook) []byte {
	return GetKeyForPrice(longBook.Entry.Price)
}

func GetKeyForPrice(price sdk.Dec) []byte {
	key, err := price.Marshal()
	if err != nil {
		panic(err)
	}
	return key
}
