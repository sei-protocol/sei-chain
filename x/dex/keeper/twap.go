package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetTwap(ctx sdk.Context, twap types.Twap, contractAddr string) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.TwapPrefix(contractAddr))
	b := k.cdc.MustMarshal(&twap)
	store.Set(types.PairPrefix(
		types.Denom(types.Denom_value[twap.PriceDenom]), types.Denom(types.Denom_value[twap.AssetDenom])), b)
}

func (k Keeper) GetTwapState(ctx sdk.Context, contractAddr string, priceDenom types.Denom, assetDenom types.Denom) types.Twap {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.TwapPrefix(contractAddr))
	b := store.Get(types.PairPrefix(priceDenom, assetDenom))
	res := types.Twap{}
	k.cdc.MustUnmarshal(b, &res)
	return res
}

func (k Keeper) GetAllTwaps(ctx sdk.Context, contractAddr string) (list []types.Twap) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.TwapPrefix(contractAddr))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Twap
		k.cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, val)
	}

	return
}
