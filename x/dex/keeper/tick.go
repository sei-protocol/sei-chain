package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// pair -> tick size
func (k Keeper) SetTickSizeForPair(ctx sdk.Context, pair types.Pair, ticksize sdk.Dec) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefix("ticks"))
	bytes, err := ticksize.Marshal()
	if err != nil {
		panic(err)
	}
	store.Set(types.PairPrefix(pair.PriceDenom, pair.AssetDenom), bytes)
}

func (k Keeper) GetTickSizeForPair(ctx sdk.Context, pair types.Pair) (sdk.Dec, bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefix("ticks"))
	b := store.Get(types.PairPrefix(pair.PriceDenom, pair.AssetDenom))
	if b == nil {
		return sdk.ZeroDec(), false
	}
	res := sdk.Dec{}
	err := res.Unmarshal(b)
	if err != nil {
		panic(err)
	}
	return res, true
}
