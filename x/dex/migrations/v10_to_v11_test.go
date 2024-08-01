package migrations_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/migrations"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestMigrate10to11(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	// write old contract
	store := ctx.KVStore(dexkeeper.GetStoreKey())
	value := []byte("test_value")
	store.Set(types.ContractKeyPrefix(types.LongBookKey, keepertest.TestContract), value)
	store.Set(types.OrderBookPrefix(false, keepertest.TestContract, "USDC", "ATOM"), value)
	store.Set(TriggerOrderBookPrefix(keepertest.TestContract, "USDC", "ATOM"), value)
	store.Set(types.PricePrefix(keepertest.TestContract, "USDC", "ATOM"), value)
	store.Set(migrations.SettlementEntryPrefix(keepertest.TestContract, "USDC", "ATOM"), value)
	store.Set(types.RegisteredPairPrefix(keepertest.TestContract), value)
	store.Set(types.OrderPrefix(keepertest.TestContract), value)
	store.Set(types.NextOrderIDPrefix(keepertest.TestContract), value)
	store.Set(migrations.NextSettlementIDPrefix(keepertest.TestContract, "USDC", "ATOM"), value)
	store.Set(types.MatchResultPrefix(keepertest.TestContract), value)
	store.Set(types.MemOrderPrefixForPair(keepertest.TestContract, "USDC", "ATOM"), value)
	store.Set(types.MemCancelPrefixForPair(keepertest.TestContract, "USDC", "ATOM"), value)
	store.Set(types.MemOrderPrefix(keepertest.TestContract), value)
	store.Set(types.MemCancelPrefix(keepertest.TestContract), value)
	store.Set(types.MemDepositPrefix(keepertest.TestContract), value)

	err := migrations.V10ToV11(ctx, *dexkeeper)
	require.NoError(t, err)

	require.False(t, store.Has(types.ContractKeyPrefix(types.LongBookKey, keepertest.TestContract)))
	require.False(t, store.Has(types.OrderBookPrefix(false, keepertest.TestContract, "USDC", "ATOM")))
	require.False(t, store.Has(TriggerOrderBookPrefix(keepertest.TestContract, "USDC", "ATOM")))
	require.False(t, store.Has(types.PricePrefix(keepertest.TestContract, "USDC", "ATOM")))
	require.False(t, store.Has(migrations.SettlementEntryPrefix(keepertest.TestContract, "USDC", "ATOM")))
	require.False(t, store.Has(types.RegisteredPairPrefix(keepertest.TestContract)))
	require.False(t, store.Has(types.OrderPrefix(keepertest.TestContract)))
	require.False(t, store.Has(types.NextOrderIDPrefix(keepertest.TestContract)))
	require.False(t, store.Has(migrations.NextSettlementIDPrefix(keepertest.TestContract, "USDC", "ATOM")))
	require.False(t, store.Has(types.MatchResultPrefix(keepertest.TestContract)))
	require.False(t, store.Has(types.MemOrderPrefixForPair(keepertest.TestContract, "USDC", "ATOM")))
	require.False(t, store.Has(types.MemCancelPrefixForPair(keepertest.TestContract, "USDC", "ATOM")))
	require.False(t, store.Has(types.MemOrderPrefix(keepertest.TestContract)))
	require.False(t, store.Has(types.MemCancelPrefix(keepertest.TestContract)))
	require.False(t, store.Has(types.MemDepositPrefix(keepertest.TestContract)))
}

func TriggerOrderBookPrefix(contractAddr string, priceDenom string, assetDenom string) []byte {
	prefix := types.KeyPrefix("TriggerBook-value-")

	return append(
		append(prefix, types.AddressKeyPrefix(contractAddr)...),
		types.PairPrefix(priceDenom, assetDenom)...,
	)
}
