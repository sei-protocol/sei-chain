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
		nextSettlementID := k.GetNextSettlementID(ctx, contractAddr, priceDenom, assetDenom, settlement.OrderId)
		settlementBytes, err := settlement.Marshal()
		if err != nil {
			panic("invalid settlement")
		}
		store.Set(GetSettlementKey(settlement.OrderId, settlement.Account, nextSettlementID), settlementBytes)
		k.SetNextSettlementID(ctx, contractAddr, priceDenom, assetDenom, settlement.OrderId, nextSettlementID+1)
	}
}

// used by grpc query
func (k Keeper) GetSettlementsState(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, account string, orderID uint64) types.Settlements {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, priceDenom, assetDenom))
	res := []*types.SettlementEntry{}
	iterator := sdk.KVStorePrefixIterator(store, GetSettlementOrderIDPrefix(orderID, account))

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.SettlementEntry
		if err := val.Unmarshal(iterator.Value()); err != nil {
			panic("invalid settlement entry")
		}
		res = append(res, &val)
	}

	return types.Settlements{Entries: res}
}

// used by grpc query
func (k Keeper) GetSettlementsStateForAccount(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, account string) []types.Settlements {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, priceDenom, assetDenom))
	res := []types.Settlements{}
	iterator := sdk.KVStorePrefixIterator(store, []byte(account))

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.SettlementEntry
		if err := val.Unmarshal(iterator.Value()); err != nil {
			panic("invalid settlement entry")
		}
		res = append(res, types.Settlements{Entries: []*types.SettlementEntry{&val}})
	}

	return res
}

// used by grpc query
func (k Keeper) GetAllSettlementsState(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, limit int) []types.Settlements {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, priceDenom, assetDenom))
	res := []types.Settlements{}
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.SettlementEntry
		if err := val.Unmarshal(iterator.Value()); err != nil {
			panic("invalid settlement entry")
		}
		res = append(res, types.Settlements{Entries: []*types.SettlementEntry{&val}})
		if len(res) >= limit {
			break
		}
	}

	return res
}

func (k Keeper) GetNextSettlementID(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, orderID uint64) uint64 {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.NextSettlementIDPrefix(contractAddr, priceDenom, assetDenom))
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, orderID)
	if !store.Has(key) {
		return 0
	}
	return binary.BigEndian.Uint64(store.Get(key))
}

func (k Keeper) SetNextSettlementID(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, orderID uint64, nextSettlementID uint64) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.NextSettlementIDPrefix(contractAddr, priceDenom, assetDenom))
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, orderID)
	value := make([]byte, 8)
	binary.BigEndian.PutUint64(value, nextSettlementID)
	store.Set(key, value)
}

func GetSettlementOrderIDPrefix(orderID uint64, account string) []byte {
	accountBytes := append([]byte(account), []byte("|")...)
	orderIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(orderIDBytes, orderID)
	return append(accountBytes, orderIDBytes...)
}

func GetSettlementKey(orderID uint64, account string, settlementID uint64) []byte {
	settlementIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(settlementIDBytes, settlementID)
	return append(GetSettlementOrderIDPrefix(orderID, account), settlementIDBytes...)
}
