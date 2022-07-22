package keeper

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetSettlements(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, settlements types.Settlements) {
	for _, settlement := range settlements.GetEntries() {
		store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, priceDenom, assetDenom, settlement.Account))
		existing, found := k.GetSettlementsState(ctx, contractAddr, priceDenom, assetDenom, settlement.Account, settlement.OrderId)
		if found {
			existing.Entries = append(existing.Entries, settlement)
		} else {
			existing = settlements
		}

		orderIDBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(orderIDBytes, settlement.OrderId)
		b := k.Cdc.MustMarshal(&existing)
		store.Set(orderIDBytes, b)
	}
}

func (k Keeper) GetSettlementsState(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, account string, orderID uint64) (val types.Settlements, found bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, priceDenom, assetDenom, account))
	orderIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(orderIDBytes, orderID)
	b := store.Get(orderIDBytes)
	val = types.Settlements{}
	if b == nil {
		return val, false
	}
	k.Cdc.MustUnmarshal(b, &val)
	return val, true
}

func (k Keeper) GetSettlementsStateForAccount(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, account string) []types.Settlements {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, priceDenom, assetDenom, account))
	res := []types.Settlements{}
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Settlements
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		res = append(res, val)
	}

	return res
}
