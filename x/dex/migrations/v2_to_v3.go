package migrations

import (
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const CONTRACT_ADDRESS_LENGTH = 62

/**
 * No `dex` state exists in any public chain at the time this data type update happened.
 * Any new chain (including local ones) should be based on a Sei version newer than this update
 * and therefore doesn't need this migration
 */
func DataTypeUpdate(ctx sdk.Context, storeKey sdk.StoreKey, cdc codec.BinaryCodec) error {
	ClearStore(ctx, storeKey)
	return nil
}

/**
 * CAUTION: this function clears up the entire `dex` module store, so it should only ever
 *          be used outside of a production setting.
 */
func ClearStore(ctx sdk.Context, storeKey sdk.StoreKey) {
	store := ctx.KVStore(storeKey)
	iterator := sdk.KVStorePrefixIterator(store, []byte{})
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		store.Delete(iterator.Key())
	}
}

// func MigrateLongBooks(ctx sdk.Context, storeKey sdk.StoreKey, cdc codec.BinaryCodec) error {
// 	longBookKey := types.KeyPrefix(types.LongBookKey)
// 	store := prefix.NewStore(ctx.KVStore(storeKey), longBookKey)
// 	iterator := sdk.KVStorePrefixIterator(store, []byte{})

// 	defer iterator.Close()
// 	for ; iterator.Valid(); iterator.Next() {
// 		var val legacytypes.LongBook
// 		cdc.MustUnmarshal(iterator.Value(), &val)
// 		key := iterator.Key()
// 		store.Delete(key)
// 		priceDenom, priceUnit, err := types.GetDenomFromStr(val.Entry.PriceDenom)
// 		if err != nil {
// 			continue
// 		}
// 		assetDenom, assetUnit, err := types.GetDenomFromStr(val.Entry.AssetDenom)
// 		if err != nil {
// 			continue
// 		}
// 		price := types.ConvertDecToStandard(priceUnit, newDecFromUint64(val.Entry.Price))
// 		allocations := []sdk.Dec{}
// 		for _, allo := range val.Entry.Allocation {
// 			allocations = append(allocations, types.ConvertDecToStandard(assetUnit, newDecFromUint64(allo)))
// 		}
// 		newKey := append(
// 			key[:CONTRACT_ADDRESS_LENGTH],
// 			append(
// 				types.PairPrefix(priceDenom, assetDenom),
// 				keeper.GetKeyForPrice(price)...,
// 			)...,
// 		)
// 		newVal := types.LongBook{
// 			Price: price,
// 			Entry: &types.OrderEntry{
// 				Price:             price,
// 				Quantity:          types.ConvertDecToStandard(assetUnit, newDecFromUint64(val.Entry.Quantity)),
// 				AllocationCreator: val.Entry.AllocationCreator,
// 				Allocation:        allocations,
// 				PriceDenom:        priceDenom,
// 				AssetDenom:        assetDenom,
// 			},
// 		}
// 		store.Set(newKey, cdc.MustMarshal(&newVal))
// 	}
// 	return nil
// }

// func MigrateShortBooks(ctx sdk.Context, storeKey sdk.StoreKey, cdc codec.BinaryCodec) error {
// 	shortBookKey := types.KeyPrefix(types.ShortBookKey)
// 	store := prefix.NewStore(ctx.KVStore(storeKey), shortBookKey)
// 	iterator := sdk.KVStorePrefixIterator(store, []byte{})

// 	defer iterator.Close()
// 	for ; iterator.Valid(); iterator.Next() {
// 		var val legacytypes.ShortBook
// 		cdc.MustUnmarshal(iterator.Value(), &val)
// 		key := iterator.Key()
// 		store.Delete(key)
// 		priceDenom, priceUnit, err := types.GetDenomFromStr(val.Entry.PriceDenom)
// 		if err != nil {
// 			continue
// 		}
// 		assetDenom, assetUnit, err := types.GetDenomFromStr(val.Entry.AssetDenom)
// 		if err != nil {
// 			continue
// 		}
// 		price := types.ConvertDecToStandard(priceUnit, newDecFromUint64(val.Entry.Price))
// 		allocations := []sdk.Dec{}
// 		for _, allo := range val.Entry.Allocation {
// 			allocations = append(allocations, types.ConvertDecToStandard(assetUnit, newDecFromUint64(allo)))
// 		}
// 		newKey := append(
// 			key[:CONTRACT_ADDRESS_LENGTH],
// 			append(
// 				types.PairPrefix(priceDenom, assetDenom),
// 				keeper.GetKeyForPrice(price)...,
// 			)...,
// 		)
// 		newVal := types.ShortBook{
// 			Price: price,
// 			Entry: &types.OrderEntry{
// 				Price:             price,
// 				Quantity:          types.ConvertDecToStandard(assetUnit, newDecFromUint64(val.Entry.Quantity)),
// 				AllocationCreator: val.Entry.AllocationCreator,
// 				Allocation:        allocations,
// 				PriceDenom:        priceDenom,
// 				AssetDenom:        assetDenom,
// 			},
// 		}
// 		store.Set(newKey, cdc.MustMarshal(&newVal))
// 	}
// 	return nil
// }

// func MigrateSettlements(ctx sdk.Context, storeKey sdk.StoreKey, cdc codec.BinaryCodec) error {
// 	settlementKey := types.KeyPrefix(types.SettlementEntryKey)
// 	store := prefix.NewStore(ctx.KVStore(storeKey), settlementKey)
// 	iterator := sdk.KVStorePrefixIterator(store, []byte{})

// 	defer iterator.Close()
// 	for ; iterator.Valid(); iterator.Next() {
// 		var val legacytypes.Settlements
// 		cdc.MustUnmarshal(iterator.Value(), &val)
// 		key := iterator.Key()
// 		store.Delete(key)
// 		if len(val.Entries) == 0 {
// 			continue
// 		}
// 		priceDenom, priceUnit, err := types.GetDenomFromStr(val.Entries[0].PriceDenom)
// 		if err != nil {
// 			continue
// 		}
// 		assetDenom, assetUnit, err := types.GetDenomFromStr(val.Entries[0].AssetDenom)
// 		if err != nil {
// 			continue
// 		}
// 		newKey := append(
// 			key[:CONTRACT_ADDRESS_LENGTH+8],
// 			types.PairPrefix(priceDenom, assetDenom)...,
// 		)
// 		newVal := types.Settlements{
// 			Epoch:   val.Epoch,
// 			Entries: []*types.SettlementEntry{},
// 		}
// 		for _, entry := range val.Entries {
// 			newVal.Entries = append(newVal.Entries, &types.SettlementEntry{
// 				Account:                entry.Account,
// 				PriceDenom:             entry.PriceDenom,
// 				AssetDenom:             entry.AssetDenom,
// 				Quantity:               types.ConvertDecToStandard(assetUnit, sdk.MustNewDecFromStr(entry.Quantity)),
// 				ExecutionCostOrProceed: types.ConvertDecToStandard(priceUnit, sdk.MustNewDecFromStr(entry.ExecutionCostOrProceed)),
// 				ExpectedCostOrProceed:  types.ConvertDecToStandard(priceUnit, sdk.MustNewDecFromStr(entry.ExpectedCostOrProceed)),
// 				PositionDirection:      entry.PositionDirection,
// 				PositionEffect:         entry.PositionEffect,
// 				Leverage:               sdk.MustNewDecFromStr(entry.Leverage),
// 			})
// 		}
// 		store.Set(newKey, cdc.MustMarshal(&newVal))
// 	}
// 	return nil
// }

// func MigrateTwap(ctx sdk.Context, storeKey sdk.StoreKey, cdc codec.BinaryCodec) error {
// 	// module-level twap will be deprecated, so simply deleting here
// 	twapKey := types.KeyPrefix(types.TwapKey)
// 	store := prefix.NewStore(ctx.KVStore(storeKey), twapKey)
// 	iterator := sdk.KVStorePrefixIterator(store, []byte{})

// 	defer iterator.Close()
// 	for ; iterator.Valid(); iterator.Next() {
// 		key := iterator.Key()
// 		store.Delete(key)
// 	}
// 	return nil
// }

// func MigrateRegisteredPairs(ctx sdk.Context, storeKey sdk.StoreKey, cdc codec.BinaryCodec) error {
// 	pairKey := types.KeyPrefix(types.RegisteredPairKey)
// 	store := prefix.NewStore(ctx.KVStore(storeKey), pairKey)
// 	iterator := sdk.KVStorePrefixIterator(store, []byte{})

// 	defer iterator.Close()
// 	for ; iterator.Valid(); iterator.Next() {
// 		key := iterator.Key()
// 		if string(key[:3]) == "cnt" {
// 			continue
// 		}
// 		var val legacytypes.Pair
// 		cdc.MustUnmarshal(iterator.Value(), &val)
// 		store.Delete(key)
// 		priceDenom, _, err := types.GetDenomFromStr(val.PriceDenom)
// 		if err != nil {
// 			continue
// 		}
// 		assetDenom, _, err := types.GetDenomFromStr(val.AssetDenom)
// 		if err != nil {
// 			continue
// 		}
// 		newVal := types.Pair{
// 			PriceDenom: priceDenom,
// 			AssetDenom: assetDenom,
// 		}
// 		store.Set(key, cdc.MustMarshal(&newVal))
// 	}
// 	return nil
// }

// func newDecFromUint64(val uint64) sdk.Dec {
// 	return sdk.NewDecFromInt(sdk.NewInt(int64(val)))
// }
