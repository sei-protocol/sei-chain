package keeper

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) AddNewOrder(ctx sdk.Context, order types.Order) {
	k.storeOrder(ctx, order)
	k.addAccountActiveOrder(ctx, order)
}

func (k Keeper) storeOrder(ctx sdk.Context, order types.Order) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.OrderPrefix(order.ContractAddr),
	)
	b := k.Cdc.MustMarshal(&order)
	idKey := make([]byte, 8)
	binary.BigEndian.PutUint64(idKey, order.Id)
	store.Set(idKey, b)
}

func (k Keeper) addAccountActiveOrder(ctx sdk.Context, order types.Order) {
	activeOrders := k.GetAccountActiveOrders(ctx, order.ContractAddr, order.Account)
	activeOrders.Ids = append(activeOrders.Ids, order.Id)
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.AccountActiveOrdersPrefix(order.ContractAddr),
	)
	accountKey := []byte(order.Account)
	b := k.Cdc.MustMarshal(activeOrders)
	store.Set(accountKey, b)
}

func (k Keeper) AddCancel(ctx sdk.Context, contractAddr string, cancel types.Cancellation) {
	originalOrder := k.GetOrdersByIds(ctx, contractAddr, []uint64{cancel.Id})[cancel.Id]
	k.storeCancel(ctx, cancel, originalOrder)
	k.RemoveAccountActiveOrder(ctx, cancel.Id, originalOrder.ContractAddr, originalOrder.Account)
}

func (k Keeper) storeCancel(ctx sdk.Context, cancel types.Cancellation, originalOrder types.Order) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.Cancel(originalOrder.ContractAddr),
	)
	b := k.Cdc.MustMarshal(&cancel)
	idKey := make([]byte, 8)
	binary.BigEndian.PutUint64(idKey, cancel.Id)
	store.Set(idKey, b)
}

func (k Keeper) RemoveAccountActiveOrder(ctx sdk.Context, orderID uint64, contractAddr string, account string) {
	activeOrders := k.GetAccountActiveOrders(ctx, contractAddr, account)
	activeOrders.Ids = utils.FilterUInt64Slice(activeOrders.Ids, orderID)
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.AccountActiveOrdersPrefix(contractAddr),
	)
	accountKey := []byte(account)
	b := k.Cdc.MustMarshal(activeOrders)
	store.Set(accountKey, b)
}

func (k Keeper) GetOrdersByIds(ctx sdk.Context, contractAddr string, ids []uint64) map[uint64]types.Order {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.OrderPrefix(contractAddr),
	)
	res := map[uint64]types.Order{}
	for _, id := range ids {
		idKey := make([]byte, 8)
		binary.BigEndian.PutUint64(idKey, id)
		if !store.Has(idKey) {
			continue
		}
		order := types.Order{}
		k.Cdc.MustUnmarshal(store.Get(idKey), &order)
		res[id] = order
	}
	return res
}

func (k Keeper) GetAccountActiveOrders(ctx sdk.Context, contractAddr string, account string) *types.ActiveOrders {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		types.AccountActiveOrdersPrefix(contractAddr),
	)
	accountKey := []byte(account)
	if !store.Has(accountKey) {
		return &types.ActiveOrders{Ids: []uint64{}}
	}
	res := types.ActiveOrders{}
	k.Cdc.MustUnmarshal(store.Get(accountKey), &res)
	return &res
}
