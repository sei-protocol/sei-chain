package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) AddRegisteredPair(ctx sdk.Context, contractAddr string, pair types.Pair) bool {
	// only add pairs that haven't been added before
	if k.HasRegisteredPair(ctx, contractAddr, pair.PriceDenom, pair.AssetDenom) {
		return false
	}
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.RegisteredPairPrefix(contractAddr))
	store.Set(types.PairPrefix(pair.PriceDenom, pair.AssetDenom), k.Cdc.MustMarshal(&pair))
	return true
}

func (k Keeper) HasRegisteredPair(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string) bool {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.RegisteredPairPrefix(contractAddr))
	return store.Has(types.PairPrefix(priceDenom, assetDenom))
}

func (k Keeper) GetRegisteredPair(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string) (types.Pair, bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.RegisteredPairPrefix(contractAddr))
	b := store.Get(types.PairPrefix(priceDenom, assetDenom))
	res := types.Pair{}
	if b == nil {
		return res, false
	}
	err := res.Unmarshal(b)
	if err != nil {
		panic(err)
	}
	return res, true
}

func (k Keeper) GetAllRegisteredPairs(ctx sdk.Context, contractAddr string) []types.Pair {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.RegisteredPairPrefix(contractAddr))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	list := []types.Pair{}
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Pair
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, val)
	}

	return list
}

func (k Keeper) DeleteAllRegisteredPairsForContract(ctx sdk.Context, contractAddr string) {
	k.removeAllForPrefix(ctx, types.RegisteredPairPrefix(contractAddr))
}
