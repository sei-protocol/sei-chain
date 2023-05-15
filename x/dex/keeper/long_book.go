package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
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

func (k Keeper) SetLongOrderBookEntry(ctx sdk.Context, contractAddr string, longBook types.OrderBookEntry) {
	k.SetLongBook(ctx, contractAddr, *longBook.(*types.LongBook))
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

func (k Keeper) GetLongOrderBookEntryByPrice(ctx sdk.Context, contractAddr string, price sdk.Dec, priceDenom string, assetDenom string) (types.OrderBookEntry, bool) {
	entry, found := k.GetLongBookByPrice(ctx, contractAddr, price, priceDenom, assetDenom)
	return &entry, found
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

func (k Keeper) GetAllLongBookForPair(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string) (list []types.OrderBookEntry) {
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

func (k Keeper) GetTopNLongBooksForPair(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, n int) (list []types.OrderBookEntry) {
	if n == 0 {
		return
	}
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.OrderBookPrefix(true, contractAddr, priceDenom, assetDenom))
	iterator := sdk.KVStoreReversePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.LongBook
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, &val)
		if len(list) == n {
			break
		}
	}

	return
}

// Load the first (up to) N long book entries whose price are smaller than the specified limit
// in reverse sorted order.
// Parameters:
//
//	n: the largest number of entries to load
//	startExclusive: the price limit
func (k Keeper) GetTopNLongBooksForPairStarting(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, n int, startExclusive sdk.Dec) (list []types.OrderBookEntry) {
	if n == 0 {
		return
	}
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.OrderBookPrefix(true, contractAddr, priceDenom, assetDenom))
	iterator := sdk.KVStoreReversePrefixIterator(store, []byte{})

	defer iterator.Close()

	// Fast-forward
	// TODO: add iterator interface that allows starting at a certain subkey under prefix
	for ; iterator.Valid(); iterator.Next() {
		key := dexutils.BytesToDec(iterator.Key())
		if key.LT(startExclusive) {
			break
		}
	}

	for ; iterator.Valid(); iterator.Next() {
		var val types.LongBook
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, &val)
		if len(list) == n {
			break
		}
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

func (k Keeper) GetLongAllocationForOrderID(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, price sdk.Dec, orderID uint64) (*types.Allocation, bool) {
	orderBook, found := k.GetLongBookByPrice(ctx, contractAddr, price, priceDenom, assetDenom)
	if !found {
		return nil, false
	}
	for _, allocation := range orderBook.Entry.Allocations {
		if allocation.OrderId == orderID {
			return allocation, true
		}
	}
	return nil, false
}

func (k Keeper) RemoveAllLongBooksForContract(ctx sdk.Context, contractAddr string) {
	k.removeAllForPrefix(ctx, types.OrderBookContractPrefix(true, contractAddr))
}

func GetKeyForLongBook(longBook types.LongBook) []byte {
	return GetKeyForPrice(longBook.Entry.Price)
}

func GetKeyForPrice(price sdk.Dec) []byte {
	return dexutils.DecToBigEndian(price)
}
