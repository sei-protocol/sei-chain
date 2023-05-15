package keeper

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetPriceState(ctx sdk.Context, price types.Price, contractAddr string) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PricePrefix(contractAddr, price.Pair.PriceDenom, price.Pair.AssetDenom))
	b := k.Cdc.MustMarshal(&price)
	store.Set(GetKeyForTs(price.SnapshotTimestampInSeconds), b)
}

func (k Keeper) DeletePriceStateBefore(ctx sdk.Context, contractAddr string, timestamp uint64, pair types.Pair) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PricePrefix(contractAddr, pair.PriceDenom, pair.AssetDenom))
	for _, key := range k.GetPriceKeysToDelete(store, timestamp) {
		store.Delete(key)
	}
}

func (k Keeper) GetPriceKeysToDelete(store sdk.KVStore, timestamp uint64) [][]byte {
	keys := [][]byte{}
	iterator := sdk.KVStorePrefixIterator(store, []byte{})
	defer iterator.Close()

	// Since timestamp is encoded in big endian, the first price being iterated has the smallest timestamp.
	for ; iterator.Valid(); iterator.Next() {
		priceKey := iterator.Key()
		priceTs := binary.BigEndian.Uint64(priceKey)
		if priceTs < timestamp {
			keys = append(keys, priceKey)
		} else {
			break
		}
	}
	return keys
}

func (k Keeper) GetPriceState(ctx sdk.Context, contractAddr string, timestamp uint64, pair types.Pair) (types.Price, bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PricePrefix(contractAddr, pair.PriceDenom, pair.AssetDenom))
	res := types.Price{}
	key := GetKeyForTs(timestamp)
	if !store.Has(key) {
		res.Pair = &pair
		return res, false
	}
	b := store.Get(key)
	k.Cdc.MustUnmarshal(b, &res)
	return res, true
}

func (k Keeper) GetAllPrices(ctx sdk.Context, contractAddr string, pair types.Pair) (list []*types.Price) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PricePrefix(contractAddr, pair.PriceDenom, pair.AssetDenom))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Price
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, &val)
	}

	return
}

func (k Keeper) GetPricesForTwap(ctx sdk.Context, contractAddr string, pair types.Pair, lookback uint64) (list []*types.Price) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PricePrefix(contractAddr, pair.PriceDenom, pair.AssetDenom))
	iterator := sdk.KVStoreReversePrefixIterator(store, []byte{})

	defer iterator.Close()

	cutoff := uint64(ctx.BlockTime().Unix()) - lookback
	for ; iterator.Valid(); iterator.Next() {
		var val types.Price
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		// add to list before breaking since we want to include one older price if there is any
		list = append(list, &val)
		if val.SnapshotTimestampInSeconds < cutoff {
			break
		}
	}

	return
}

func (k Keeper) RemoveAllPricesForContract(ctx sdk.Context, contractAddr string) {
	k.removeAllForPrefix(ctx, types.PriceContractPrefix(contractAddr))
}

func GetKeyForTs(ts uint64) []byte {
	tsKey := make([]byte, 8)
	binary.BigEndian.PutUint64(tsKey, ts)
	return tsKey
}
