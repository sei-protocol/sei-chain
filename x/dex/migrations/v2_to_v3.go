package migrations

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	legacytypes "github.com/sei-protocol/sei-chain/x/dex/types/legacy/v0"
)

const CONTRACT_ADDRESS_LENGTH = 62

func getDexStore(ctx sdk.Context) sdk.KVStore {
	return ctx.KVStore(sdk.NewKVStoreKey(types.StoreKey))
}

func DataTypeUpdate(ctx sdk.Context, cdc codec.BinaryCodec) error {
	MigrateLongBooks(ctx, cdc)
	MigrateShortBooks(ctx, cdc)
	MigrateSettlements(ctx, cdc)
	MigrateTwap(ctx, cdc)
	return nil
}

func MigrateLongBooks(ctx sdk.Context, cdc codec.BinaryCodec) error {
	longBookKey := types.KeyPrefix(types.LongBookKey)
	store := prefix.NewStore(getDexStore(ctx), longBookKey)
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		var val legacytypes.LongBook
		cdc.MustUnmarshal(iterator.Value(), &val)
		key := iterator.Key()
		store.Delete(key)
		contractAddr := string(key[len(longBookKey) : len(longBookKey)+CONTRACT_ADDRESS_LENGTH])
		priceDenom, err := types.GetDenomFromStr(val.Entry.PriceDenom)
		if err != nil {
			continue
		}
		assetDenom, err := types.GetDenomFromStr(val.Entry.AssetDenom)
		if err != nil {
			continue
		}
		price := newDecFromUint64(val.Entry.Price)
		allocations := []sdk.Dec{}
		for _, allo := range val.Entry.Allocation {
			allocations = append(allocations, newDecFromUint64(allo))
		}
		newKey := append(types.OrderBookPrefix(
			true, contractAddr, priceDenom, assetDenom,
		), keeper.GetKeyForPrice(price)...)
		newVal := types.LongBook{
			Price: price,
			Entry: &types.OrderEntry{
				Price:             price,
				Quantity:          newDecFromUint64(val.Entry.Quantity),
				AllocationCreator: val.Entry.AllocationCreator,
				Allocation:        allocations,
				PriceDenom:        priceDenom,
				AssetDenom:        assetDenom,
			},
		}
		store.Set(newKey, cdc.MustMarshal(&newVal))
	}
	return nil
}

func MigrateShortBooks(ctx sdk.Context, cdc codec.BinaryCodec) error {
	shortBookKey := types.KeyPrefix(types.ShortBookKey)
	store := prefix.NewStore(getDexStore(ctx), shortBookKey)
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		var val legacytypes.ShortBook
		cdc.MustUnmarshal(iterator.Value(), &val)
		key := iterator.Key()
		store.Delete(key)
		contractAddr := string(key[len(shortBookKey) : len(shortBookKey)+CONTRACT_ADDRESS_LENGTH])
		priceDenom, err := types.GetDenomFromStr(val.Entry.PriceDenom)
		if err != nil {
			continue
		}
		assetDenom, err := types.GetDenomFromStr(val.Entry.AssetDenom)
		if err != nil {
			continue
		}
		price := newDecFromUint64(val.Entry.Price)
		allocations := []sdk.Dec{}
		for _, allo := range val.Entry.Allocation {
			allocations = append(allocations, newDecFromUint64(allo))
		}
		newKey := append(types.OrderBookPrefix(
			false, contractAddr, priceDenom, assetDenom,
		), keeper.GetKeyForPrice(price)...)
		newVal := types.ShortBook{
			Price: price,
			Entry: &types.OrderEntry{
				Price:             price,
				Quantity:          newDecFromUint64(val.Entry.Quantity),
				AllocationCreator: val.Entry.AllocationCreator,
				Allocation:        allocations,
				PriceDenom:        priceDenom,
				AssetDenom:        assetDenom,
			},
		}
		store.Set(newKey, cdc.MustMarshal(&newVal))
	}
	return nil
}

func MigrateSettlements(ctx sdk.Context, cdc codec.BinaryCodec) error {
	settlementKey := types.KeyPrefix(types.SettlementEntryKey)
	store := prefix.NewStore(getDexStore(ctx), settlementKey)
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		var val legacytypes.Settlements
		cdc.MustUnmarshal(iterator.Value(), &val)
		key := iterator.Key()
		store.Delete(key)
		if len(val.Entries) == 0 {
			continue
		}
		contractAddr := string(key[len(settlementKey) : len(settlementKey)+CONTRACT_ADDRESS_LENGTH])
		heightBytes := key[len(settlementKey)+CONTRACT_ADDRESS_LENGTH : len(settlementKey)+CONTRACT_ADDRESS_LENGTH+8]
		height := binary.BigEndian.Uint64(heightBytes)
		priceDenom, err := types.GetDenomFromStr(val.Entries[0].PriceDenom)
		if err != nil {
			continue
		}
		assetDenom, err := types.GetDenomFromStr(val.Entries[0].AssetDenom)
		if err != nil {
			continue
		}
		newKey := append(types.SettlementEntryPrefix(
			contractAddr, height,
		), types.PairPrefix(priceDenom, assetDenom)...)
		newVal := types.Settlements{
			Epoch:   val.Epoch,
			Entries: []*types.SettlementEntry{},
		}
		for _, entry := range val.Entries {
			newVal.Entries = append(newVal.Entries, &types.SettlementEntry{
				Account:                entry.Account,
				PriceDenom:             entry.PriceDenom,
				AssetDenom:             entry.AssetDenom,
				Quantity:               sdk.MustNewDecFromStr(entry.Quantity),
				ExecutionCostOrProceed: sdk.MustNewDecFromStr(entry.ExecutionCostOrProceed),
				ExpectedCostOrProceed:  sdk.MustNewDecFromStr(entry.ExpectedCostOrProceed),
				PositionDirection:      entry.PositionDirection,
				PositionEffect:         entry.PositionEffect,
				Leverage:               sdk.MustNewDecFromStr(entry.Leverage),
			})
		}
		store.Set(newKey, cdc.MustMarshal(&newVal))
	}
	return nil
}

func MigrateTwap(ctx sdk.Context, cdc codec.BinaryCodec) error {
	// module-level twap will be deprecated, so simply deleting here
	twapKey := types.KeyPrefix(types.TwapKey)
	store := prefix.NewStore(getDexStore(ctx), twapKey)
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		store.Delete(key)
	}
	return nil
}

func newDecFromUint64(val uint64) sdk.Dec {
	return sdk.NewDecFromInt(sdk.NewInt(int64(val)))
}
