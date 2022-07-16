package keeper

import (
	"encoding/binary"
	"fmt"

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
	iterator := sdk.KVStorePrefixIterator(store, []byte{})
	defer iterator.Close()

	// Since timestamp is encoded in big endian, the first price being iterated has the smallest timestamp.
	for ; iterator.Valid(); iterator.Next() {
		priceKey := iterator.Key()
		priceTs := binary.BigEndian.Uint64(priceKey)
		fmt.Printf("Price timestamp: %d\n", priceTs)
		if priceTs < timestamp {
			store.Delete(priceKey)
		} else {
			break
		}
	}
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

func GetKeyForTs(ts uint64) []byte {
	tsKey := make([]byte, 8)
	binary.BigEndian.PutUint64(tsKey, ts)
	return tsKey
}
