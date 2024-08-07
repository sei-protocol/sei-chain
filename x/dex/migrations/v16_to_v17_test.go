package migrations_test

import (
	"testing"

	"github.com/sei-protocol/goutils"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/migrations"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestMigrate16to17(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	store := ctx.KVStore(dexkeeper.GetStoreKey())
	dexkeeper.SetContract(ctx, &types.ContractInfoV2{ContractAddr: keepertest.TestContract})
	// add registered pair using the old key
	pair := types.Pair{PriceDenom: keepertest.TestPair.PriceDenom, AssetDenom: keepertest.TestPair.AssetDenom}
	store.Set(
		goutils.ImmutableAppend(
			types.RegisteredPairPrefix(keepertest.TestContract),
			migrations.OldPairPrefix(keepertest.TestPair.PriceDenom, keepertest.TestPair.AssetDenom)...,
		),
		dexkeeper.Cdc.MustMarshal(&pair),
	)

	value := []byte("test_value")
	store.Set(goutils.ImmutableAppend(
		types.OrderBookContractPrefix(true, keepertest.TestContract),
		migrations.OldPairPrefix(pair.PriceDenom, pair.AssetDenom)...,
	), value)
	store.Set(goutils.ImmutableAppend(
		types.OrderBookContractPrefix(false, keepertest.TestContract),
		migrations.OldPairPrefix(pair.PriceDenom, pair.AssetDenom)...,
	), value)
	store.Set(goutils.ImmutableAppend(
		types.PriceContractPrefix(keepertest.TestContract),
		migrations.OldPairPrefix(pair.PriceDenom, pair.AssetDenom)...,
	), value)
	store.Set(goutils.ImmutableAppend(
		goutils.ImmutableAppend(types.KeyPrefix(types.LongOrderCountKey), types.AddressKeyPrefix(keepertest.TestContract)...),
		migrations.OldPairPrefix(pair.PriceDenom, pair.AssetDenom)...,
	), value)
	store.Set(goutils.ImmutableAppend(
		goutils.ImmutableAppend(types.KeyPrefix(types.ShortOrderCountKey), types.AddressKeyPrefix(keepertest.TestContract)...),
		migrations.OldPairPrefix(pair.PriceDenom, pair.AssetDenom)...,
	), value)
	store.Set(goutils.ImmutableAppend(types.KeyPrefix(types.AssetListKey), []byte(pair.PriceDenom)...), value)

	err := migrations.V16ToV17(ctx, *dexkeeper)
	require.NoError(t, err)

	require.False(t, store.Has(goutils.ImmutableAppend(
		types.OrderBookContractPrefix(true, keepertest.TestContract),
		migrations.OldPairPrefix(pair.PriceDenom, pair.AssetDenom)...,
	)))
	require.False(t, store.Has(goutils.ImmutableAppend(
		types.OrderBookContractPrefix(false, keepertest.TestContract),
		migrations.OldPairPrefix(pair.PriceDenom, pair.AssetDenom)...,
	)))
	require.False(t, store.Has(goutils.ImmutableAppend(
		types.PriceContractPrefix(keepertest.TestContract),
		migrations.OldPairPrefix(pair.PriceDenom, pair.AssetDenom)...,
	)))
	require.False(t, store.Has(goutils.ImmutableAppend(
		goutils.ImmutableAppend(types.KeyPrefix(types.LongOrderCountKey), types.AddressKeyPrefix(keepertest.TestContract)...),
		migrations.OldPairPrefix(pair.PriceDenom, pair.AssetDenom)...,
	)))
	require.False(t, store.Has(goutils.ImmutableAppend(
		goutils.ImmutableAppend(types.KeyPrefix(types.ShortOrderCountKey), types.AddressKeyPrefix(keepertest.TestContract)...),
		migrations.OldPairPrefix(pair.PriceDenom, pair.AssetDenom)...,
	)))
	require.False(t, store.Has(goutils.ImmutableAppend(types.KeyPrefix(types.AssetListKey), []byte(pair.PriceDenom)...)))
	require.False(t, store.Has(goutils.ImmutableAppend(
		types.RegisteredPairPrefix(keepertest.TestContract),
		migrations.OldPairPrefix(keepertest.TestPair.PriceDenom, keepertest.TestPair.AssetDenom)...,
	)))

	require.True(t, store.Has(types.OrderBookPrefix(true, keepertest.TestContract, pair.PriceDenom, pair.AssetDenom)))
	require.True(t, store.Has(types.OrderBookPrefix(false, keepertest.TestContract, pair.PriceDenom, pair.AssetDenom)))
	require.True(t, store.Has(types.PricePrefix(keepertest.TestContract, pair.PriceDenom, pair.AssetDenom)))
	require.True(t, store.Has(types.OrderCountPrefix(keepertest.TestContract, pair.PriceDenom, pair.AssetDenom, true)))
	require.True(t, store.Has(types.OrderCountPrefix(keepertest.TestContract, pair.PriceDenom, pair.AssetDenom, false)))
	require.True(t, store.Has(types.AssetListPrefix(pair.PriceDenom)))
	require.True(t, store.Has(goutils.ImmutableAppend(
		types.RegisteredPairPrefix(keepertest.TestContract),
		types.PairPrefix(keepertest.TestPair.PriceDenom, keepertest.TestPair.AssetDenom)...,
	)))
}
