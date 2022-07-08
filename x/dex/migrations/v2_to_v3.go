package migrations

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	legacytypes "github.com/sei-protocol/sei-chain/x/dex/types/legacy/v0"
)

const CONTRACT_ADDRESS_LENGTH = 62

func DataTypeUpdate(ctx sdk.Context, storeKey sdk.StoreKey, cdc codec.BinaryCodec) error {
	MigrateLongBooks(ctx, storeKey, cdc)
	MigrateShortBooks(ctx, storeKey, cdc)
	MigrateSettlements(ctx, storeKey, cdc)
	MigrateTwap(ctx, storeKey, cdc)
	MigrateRegisteredPairs(ctx, storeKey, cdc)
	return nil
}

func MigrateLongBooks(ctx sdk.Context, storeKey sdk.StoreKey, cdc codec.BinaryCodec) error {
	longBookKey := types.KeyPrefix(types.LongBookKey)
	store := prefix.NewStore(ctx.KVStore(storeKey), longBookKey)
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		var val legacytypes.LongBook
		cdc.MustUnmarshal(iterator.Value(), &val)
		key := iterator.Key()
		store.Delete(key)
		priceDenom := val.Entry.PriceDenom
		assetDenom := val.Entry.AssetDenom
		price := newDecFromUint64(val.Entry.Price)
		allocations := []sdk.Dec{}
		for _, allo := range val.Entry.Allocation {
			allocations = append(allocations, newDecFromUint64(allo))
		}
		newKey := append(
			key[:CONTRACT_ADDRESS_LENGTH],
			append(
				types.PairPrefix(priceDenom, assetDenom),
				keeper.GetKeyForPrice(price)...,
			)...,
		)
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

func MigrateShortBooks(ctx sdk.Context, storeKey sdk.StoreKey, cdc codec.BinaryCodec) error {
	shortBookKey := types.KeyPrefix(types.ShortBookKey)
	store := prefix.NewStore(ctx.KVStore(storeKey), shortBookKey)
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		var val legacytypes.ShortBook
		cdc.MustUnmarshal(iterator.Value(), &val)
		key := iterator.Key()
		store.Delete(key)
		priceDenom := val.Entry.PriceDenom
		assetDenom := val.Entry.AssetDenom
		price := newDecFromUint64(val.Entry.Price)
		allocations := []sdk.Dec{}
		for _, allo := range val.Entry.Allocation {
			allocations = append(allocations, newDecFromUint64(allo))
		}
		newKey := append(
			key[:CONTRACT_ADDRESS_LENGTH],
			append(
				types.PairPrefix(priceDenom, assetDenom),
				keeper.GetKeyForPrice(price)...,
			)...,
		)
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

func MigrateSettlements(ctx sdk.Context, storeKey sdk.StoreKey, cdc codec.BinaryCodec) error {
	settlementKey := types.KeyPrefix(types.SettlementEntryKey)
	store := prefix.NewStore(ctx.KVStore(storeKey), settlementKey)
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
		priceDenom := val.Entries[0].PriceDenom
		assetDenom := val.Entries[0].AssetDenom
		newKey := append(
			key[:CONTRACT_ADDRESS_LENGTH+8],
			types.PairPrefix(priceDenom, assetDenom)...,
		)
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

func MigrateTwap(ctx sdk.Context, storeKey sdk.StoreKey, cdc codec.BinaryCodec) error {
	// module-level twap will be deprecated, so simply deleting here
	twapKey := types.KeyPrefix(types.TwapKey)
	store := prefix.NewStore(ctx.KVStore(storeKey), twapKey)
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		store.Delete(key)
	}
	return nil
}

func MigrateRegisteredPairs(ctx sdk.Context, storeKey sdk.StoreKey, cdc codec.BinaryCodec) error {
	pairKey := types.KeyPrefix(types.RegisteredPairKey)
	store := prefix.NewStore(ctx.KVStore(storeKey), pairKey)
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if string(key[:3]) == "cnt" {
			continue
		}
		var val legacytypes.Pair
		cdc.MustUnmarshal(iterator.Value(), &val)
		store.Delete(key)
		newVal := types.Pair{
			PriceDenom: val.PriceDenom,
			AssetDenom: val.AssetDenom,
		}
		store.Set(key, cdc.MustMarshal(&newVal))
	}
	return nil
}

func newDecFromUint64(val uint64) sdk.Dec {
	return sdk.NewDecFromInt(sdk.NewInt(int64(val)))
}
