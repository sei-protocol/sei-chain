package keeper

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetTriggeredOrder(ctx sdk.Context, contractAddr string, order types.Order, priceDenom string, assetDenom string) {
	store := prefix.NewStore(ctx.KVStore(k.StoreKey), types.TriggerOrderBookPrefix(contractAddr, priceDenom, assetDenom))

	b := k.Cdc.MustMarshal(&order)
	store.Set(GetKeyForOrderID(order.Id), b)
}

func (k Keeper) RemoveTriggeredOrder(ctx sdk.Context, contractAddr string, orderID uint64, priceDenom string, assetDenom string) {
	store := prefix.NewStore(ctx.KVStore(k.StoreKey), types.TriggerOrderBookPrefix(contractAddr, priceDenom, assetDenom))
	store.Delete(GetKeyForOrderID(orderID))
}

func (k Keeper) GetTriggeredOrderByID(ctx sdk.Context, contractAddr string, orderID uint64, priceDenom string, assetDenom string) (val types.Order, found bool) {
	store := prefix.NewStore(ctx.KVStore(k.StoreKey), types.TriggerOrderBookPrefix(contractAddr, priceDenom, assetDenom))
	b := store.Get(GetKeyForOrderID(orderID))
	if b == nil {
		return val, false
	}
	k.Cdc.MustUnmarshal(b, &val)
	return val, true
}

func (k Keeper) GetAllTriggeredOrdersForPair(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string) (list []types.Order) {
	store := prefix.NewStore(ctx.KVStore(k.StoreKey), types.TriggerOrderBookPrefix(contractAddr, priceDenom, assetDenom))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Order
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, val)
	}

	return
}

func (k Keeper) GetAllTriggeredOrders(ctx sdk.Context, contractAddr string) (list []types.Order) {
	store := prefix.NewStore(ctx.KVStore(k.StoreKey), types.ContractKeyPrefix(types.TriggerBookKey, contractAddr))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Order
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, val)
	}

	return
}

func GetKeyForOrderID(orderID uint64) []byte {
	key := make([]byte, 8)
	binary.LittleEndian.PutUint64(key, orderID)

	return key
}
