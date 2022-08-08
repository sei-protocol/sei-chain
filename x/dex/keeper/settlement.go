package keeper

import (
	"encoding/binary"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"time"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetSettlements(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, settlements types.Settlements) {
	executionStart := time.Now()
	defer telemetry.ModuleSetGauge(types.ModuleName, float32(time.Now().Sub(executionStart).Milliseconds()), "set_settlements_ms")
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, priceDenom, assetDenom))

	for _, settlement := range settlements.GetEntries() {
		existing, found := k.GetSettlementsState(ctx, contractAddr, priceDenom, assetDenom, settlement.Account, settlement.OrderId)
		if found {
			existing.Entries = append(existing.Entries, settlement)
		} else {
			existing = types.Settlements{
				Epoch:   settlements.Epoch,
				Entries: []*types.SettlementEntry{settlement},
			}
		}

		b := k.Cdc.MustMarshal(&existing)
		store.Set(getSettlementKey(settlement.OrderId, settlement.Account), b)
	}
}

func (k Keeper) GetSettlementsState(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, account string, orderID uint64) (val types.Settlements, found bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, priceDenom, assetDenom))
	b := store.Get(getSettlementKey(orderID, account))
	val = types.Settlements{}
	if b == nil {
		return val, false
	}
	k.Cdc.MustUnmarshal(b, &val)
	return val, true
}

func (k Keeper) GetSettlementsStateForAccount(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, account string) []types.Settlements {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, priceDenom, assetDenom))
	res := []types.Settlements{}
	iterator := sdk.KVStorePrefixIterator(store, []byte(account))

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Settlements
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		res = append(res, val)
	}

	return res
}

func (k Keeper) GetAllSettlementsState(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, limit int) []types.Settlements {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, priceDenom, assetDenom))
	res := []types.Settlements{}
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Settlements
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		res = append(res, val)
		if len(res) >= limit {
			break
		}
	}

	return res
}

func getSettlementKey(orderID uint64, account string) []byte {
	accountBytes := append([]byte(account), []byte("|")...)
	orderIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(orderIDBytes, orderID)
	return append(accountBytes, orderIDBytes...)
}
