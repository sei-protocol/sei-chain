package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetTwap(ctx sdk.Context, twap types.Twap, contractAddr string) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.TwapPrefix(contractAddr))
	b := k.Cdc.MustMarshal(&twap)
	priceDenom, _, err := types.GetDenomFromStr(twap.PriceDenom)
	if err != nil {
		panic(err)
	}
	assetDenom, _, err := types.GetDenomFromStr(twap.AssetDenom)
	if err != nil {
		panic(err)
	}
	store.Set(types.PairPrefix(priceDenom, assetDenom), b)
}

func GetKeyForTwap(priceDenom string, assetDenom string) []byte {
	return append([]byte(priceDenom), []byte(assetDenom)...)
}
