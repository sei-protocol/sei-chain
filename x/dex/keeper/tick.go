package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const defaultAddr = "default"

// contract_addr, pair -> tick size
func (k Keeper) SetTickSizeForPair(ctx sdk.Context, contractAddr string, pair types.Pair, ticksize sdk.Dec) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.TickSizeKeyPrefix(contractAddr))
	bytes, err := ticksize.Marshal()
	if err != nil {
		panic(err)
	}
	store.Set(types.PairPrefix(pair.PriceDenom, pair.AssetDenom), bytes)
}

func (k Keeper) SetDefaultTickSizeForPair(ctx sdk.Context, pair types.Pair, ticksize sdk.Dec) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.TickSizeKeyPrefix(defaultAddr))
	bytes, err := ticksize.Marshal()
	if err != nil {
		panic(err)
	}
	store.Set(types.PairPrefix(pair.PriceDenom, pair.AssetDenom), bytes)
}

func (k Keeper) GetTickSizeForPair(ctx sdk.Context, contractAddr string, pair types.Pair) (sdk.Dec, bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.TickSizeKeyPrefix(contractAddr))
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

func (k Keeper) GetDefaultTickSizeForPair(ctx sdk.Context, pair types.Pair) (sdk.Dec, bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.TickSizeKeyPrefix(defaultAddr))
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
