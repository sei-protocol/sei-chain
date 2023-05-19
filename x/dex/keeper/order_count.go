package keeper

import (
	"encoding/binary"
	"fmt"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetOrderCount(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, direction types.PositionDirection, price sdk.Dec, count uint64) error {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.OrderCountPrefix(contractAddr, priceDenom, assetDenom, direction == types.PositionDirection_LONG),
	)
	key, err := price.Marshal()
	if err != nil {
		return err
	}
	value := make([]byte, 8)
	binary.BigEndian.PutUint64(value, count)
	store.Set(key, value)
	return nil
}

func (k Keeper) GetOrderCountState(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, direction types.PositionDirection, price sdk.Dec) uint64 {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.OrderCountPrefix(contractAddr, priceDenom, assetDenom, direction == types.PositionDirection_LONG),
	)
	key, err := price.Marshal()
	if err != nil {
		ctx.Logger().Error(fmt.Sprintf("error marshal provided price %s due to %s", price.String(), err))
		return 0
	}
	value := store.Get(key)
	if value == nil {
		return 0
	}
	return binary.BigEndian.Uint64(value)
}

func (k Keeper) DecreaseOrderCount(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, direction types.PositionDirection, price sdk.Dec, count uint64) error {
	oldCount := k.GetOrderCountState(ctx, contractAddr, priceDenom, assetDenom, direction, price)
	newCount := uint64(0)
	if oldCount > count {
		newCount = oldCount - count
	}
	return k.SetOrderCount(ctx, contractAddr, priceDenom, assetDenom, direction, price, newCount)
}

func (k Keeper) IncreaseOrderCount(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, direction types.PositionDirection, price sdk.Dec, count uint64) error {
	oldCount := k.GetOrderCountState(ctx, contractAddr, priceDenom, assetDenom, direction, price)
	return k.SetOrderCount(ctx, contractAddr, priceDenom, assetDenom, direction, price, oldCount+count)
}
