package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const defaultAddr = "default"

// contract_addr, pair -> tick size
func (k Keeper) SetPriceTickSizeForPair(ctx sdk.Context, contractAddr string, pair types.Pair, ticksize sdk.Dec) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PriceTickSizeKeyPrefix(contractAddr))
	bytes, err := ticksize.Marshal()
	if err != nil {
		panic(err)
	}
	store.Set(types.PairPrefix(pair.PriceDenom, pair.AssetDenom), bytes)
}

func (k Keeper) SetDefaultPriceTickSizeForPair(ctx sdk.Context, pair types.Pair, ticksize sdk.Dec) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PriceTickSizeKeyPrefix(defaultAddr))
	bytes, err := ticksize.Marshal()
	if err != nil {
		panic(err)
	}
	store.Set(types.PairPrefix(pair.PriceDenom, pair.AssetDenom), bytes)
}

func (k Keeper) GetPriceTickSizeForPair(ctx sdk.Context, contractAddr string, pair types.Pair) (sdk.Dec, bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PriceTickSizeKeyPrefix(contractAddr))
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

func (k Keeper) GetDefaultPriceTickSizeForPair(ctx sdk.Context, pair types.Pair) (sdk.Dec, bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.PriceTickSizeKeyPrefix(defaultAddr))
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

// contract_addr, pair -> tick size
func (k Keeper) SetQuantityTickSizeForPair(ctx sdk.Context, contractAddr string, pair types.Pair, ticksize sdk.Dec) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.QuantityTickSizeKeyPrefix(contractAddr))
	bytes, err := ticksize.Marshal()
	if err != nil {
		panic(err)
	}
	store.Set(types.PairPrefix(pair.PriceDenom, pair.AssetDenom), bytes)
}

func (k Keeper) SetDefaultQuantityTickSizeForPair(ctx sdk.Context, pair types.Pair, ticksize sdk.Dec) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.QuantityTickSizeKeyPrefix(defaultAddr))
	bytes, err := ticksize.Marshal()
	if err != nil {
		panic(err)
	}
	store.Set(types.PairPrefix(pair.PriceDenom, pair.AssetDenom), bytes)
}

func (k Keeper) GetQuantityTickSizeForPair(ctx sdk.Context, contractAddr string, pair types.Pair) (sdk.Dec, bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.QuantityTickSizeKeyPrefix(contractAddr))
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

func (k Keeper) GetDefaultQuantityTickSizeForPair(ctx sdk.Context, pair types.Pair) (sdk.Dec, bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.QuantityTickSizeKeyPrefix(defaultAddr))
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
