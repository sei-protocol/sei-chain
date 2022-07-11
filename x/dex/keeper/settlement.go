package keeper

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetSettlements(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, settlements types.Settlements) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, priceDenom, assetDenom))
	for _, settlement := range settlements.GetEntries() {
		existing, found := k.GetSettlementsState(ctx, contractAddr, priceDenom, assetDenom, settlement.OrderId)
		if found {
			existing.Entries = append(existing.Entries, settlement)
		} else {
			existing = settlements
		}

		orderIdBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(orderIdBytes, settlement.OrderId)
		b := k.Cdc.MustMarshal(&existing)
		store.Set(orderIdBytes, b)
	}
}

func (k Keeper) GetSettlementsState(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, orderId uint64) (val types.Settlements, found bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, priceDenom, assetDenom))
	orderIdBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(orderIdBytes, orderId)
	b := store.Get(orderIdBytes)
	val = types.Settlements{}
	if b == nil {
		return val, false
	}
	k.Cdc.MustUnmarshal(b, &val)
	return val, true
}
