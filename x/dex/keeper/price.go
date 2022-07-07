package keeper

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetPriceState(ctx sdk.Context, price types.Price, contractAddr string, epoch uint64) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PricePrefix(contractAddr))
	b := k.Cdc.MustMarshal(&price)
	store.Set(GetKeyForPriceState(epoch, *price.Pair), b)
}

func (k Keeper) DeletePriceState(ctx sdk.Context, contractAddr string, epoch uint64, pair types.Pair) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PricePrefix(contractAddr))
	store.Delete(GetKeyForPriceState(epoch, pair))
}

func (k Keeper) GetPriceState(ctx sdk.Context, contractAddr string, epoch uint64, pair types.Pair) (types.Price, bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PricePrefix(contractAddr))
	res := types.Price{}
	key := GetKeyForPriceState(epoch, pair)
	if !store.Has(key) {
		res.Pair = &pair
		return res, false
	}
	b := store.Get(key)
	k.Cdc.MustUnmarshal(b, &res)
	return res, true
}

func (k Keeper) GetAllPrices(ctx sdk.Context, contractAddr string, pair types.Pair) (list []*types.Price) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PricePrefix(contractAddr))
	iterator := sdk.KVStorePrefixIterator(store, types.PairPrefix(
		types.GetContractDenomName(pair.PriceDenom),
		types.GetContractDenomName(pair.AssetDenom),
	))

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Price
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, &val)
	}

	return
}

func GetKeyForEpoch(epoch uint64) []byte {
	epochKey := make([]byte, 8)
	binary.BigEndian.PutUint64(epochKey, epoch)
	return epochKey
}

func GetKeyForPriceState(epoch uint64, pair types.Pair) []byte {
	return append(
		types.PairPrefix(
			types.GetContractDenomName(pair.PriceDenom),
			types.GetContractDenomName(pair.AssetDenom),
		),
		GetKeyForEpoch(epoch)...,
	)
}
