package keeper

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetPriceState(ctx sdk.Context, price types.Price, contractAddr string) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PricePrefix(contractAddr))
	b := k.cdc.MustMarshal(&price)
	store.Set(GetKeyForPriceState(price.Epoch, price.PriceDenom, price.AssetDenom), b)
}

func (k Keeper) GetPriceState(ctx sdk.Context, contractAddr string, epoch uint64, priceDenom string, assetDenom string) (types.Price, bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PricePrefix(contractAddr))
	res := types.Price{}
	key := GetKeyForPriceState(epoch, priceDenom, assetDenom)
	if !store.Has(key) {
		res.Epoch = epoch
		res.PriceDenom = priceDenom
		res.AssetDenom = assetDenom
		return res, false
	}
	b := store.Get(key)
	k.cdc.MustUnmarshal(b, &res)
	return res, true
}

func (k Keeper) GetAllPrices(ctx sdk.Context, contractAddr string, epoch uint64) (list []types.Price) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PricePrefix(contractAddr))
	iterator := sdk.KVStorePrefixIterator(store, GetKeyForEpoch(epoch))

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Price
		k.cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, val)
	}

	return
}

func GetKeyForEpoch(epoch uint64) []byte {
	epochKey := make([]byte, 8)
	binary.BigEndian.PutUint64(epochKey, epoch)
	return epochKey
}

func GetKeyForPriceState(epoch uint64, priceDenom string, assetDenom string) []byte {
	return append(
		GetKeyForEpoch(epoch),
		append([]byte(priceDenom), []byte(assetDenom)...)...,
	)
}
