package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// contract_addr, pair -> tick size
func (k Keeper) SetPriceTickSizeForPair(ctx sdk.Context, contractAddr string, pair types.Pair, ticksize sdk.Dec) error {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.RegisteredPairPrefix(contractAddr))

	pair, found := k.GetRegisteredPair(ctx, contractAddr, pair.PriceDenom, pair.AssetDenom)
	if !found {
		return types.ErrPairNotRegistered
	}
	pair.PriceTicksize = &ticksize
	store.Set(types.PairPrefix(pair.PriceDenom, pair.AssetDenom), k.Cdc.MustMarshal(&pair))

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeSetPriceTickSize,
		sdk.NewAttribute(types.AttributeKeyContractAddress, contractAddr),
	))
	return nil
}

func (k Keeper) GetPriceTickSizeForPair(ctx sdk.Context, contractAddr string, pair types.Pair) (sdk.Dec, bool) {
	pair, found := k.GetRegisteredPair(ctx, contractAddr, pair.PriceDenom, pair.AssetDenom)
	if !found {
		return sdk.ZeroDec(), false
	}
	return *pair.PriceTicksize, true
}

// contract_addr, pair -> tick size
func (k Keeper) SetQuantityTickSizeForPair(ctx sdk.Context, contractAddr string, pair types.Pair, ticksize sdk.Dec) error {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.RegisteredPairPrefix(contractAddr))

	pair, found := k.GetRegisteredPair(ctx, contractAddr, pair.PriceDenom, pair.AssetDenom)
	if !found {
		return types.ErrPairNotRegistered
	}
	pair.QuantityTicksize = &ticksize
	store.Set(types.PairPrefix(pair.PriceDenom, pair.AssetDenom), k.Cdc.MustMarshal(&pair))

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeSetQuantityTickSize,
		sdk.NewAttribute(types.AttributeKeyContractAddress, contractAddr),
	))
	return nil
}

func (k Keeper) GetQuantityTickSizeForPair(ctx sdk.Context, contractAddr string, pair types.Pair) (sdk.Dec, bool) {
	pair, found := k.GetRegisteredPair(ctx, contractAddr, pair.PriceDenom, pair.AssetDenom)
	if !found {
		return sdk.ZeroDec(), false
	}
	return *pair.QuantityTicksize, true
}
