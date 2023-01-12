package migrations_test

import (
	"encoding/binary"
	"testing"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkaddress "github.com/cosmos/cosmos-sdk/types/address"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/migrations"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestMigrate9to10(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	// write old contract
	dexStore := ctx.KVStore(dexkeeper.GetStoreKey())
	rpStore := prefix.NewStore(
		dexStore,
		[]byte(types.RegisteredPairKey),
	)
	priceTickSize := sdk.MustNewDecFromStr("0.0001")
	quantityTickSize := sdk.MustNewDecFromStr("0.0001")
	pair := types.Pair{
		PriceDenom:       keepertest.TestPair.PriceDenom,
		AssetDenom:       keepertest.TestPair.AssetDenom,
		PriceTicksize:    &priceTickSize,
		QuantityTicksize: &quantityTickSize,
	}
	pairPrefix := types.PairPrefix(keepertest.TestPair.PriceDenom, keepertest.TestPair.AssetDenom)

	pairBytes := dexkeeper.Cdc.MustMarshal(&pair)
	countBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(countBytes, 1)
	// simulate legacy store where registered pairs are indexed by auto increment count
	address, _ := sdk.AccAddressFromBech32(keepertest.TestContract)
	address = sdkaddress.MustLengthPrefix(address)
	rpStore.Set(append(address, countBytes...), pairBytes)

	bytes := rpStore.Get(append(address, countBytes...))
	require.Equal(t, pairBytes, bytes)

	// set count, ticksize, and quantity size
	newCountBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(newCountBytes, 2)
	dexStore.Set(
		append([]byte(migrations.RegisteredPairCount), address...),
		newCountBytes,
	)

	tickBytes, _ := sdk.MustNewDecFromStr("0.0002").Marshal()
	dexStore.Set(
		append(append([]byte(migrations.PriceTickSizeKey), address...), pairPrefix...),
		tickBytes,
	)

	dexStore.Set(
		append(append([]byte(migrations.QuantityTickSizeKey), address...), pairPrefix...),
		tickBytes,
	)

	err := migrations.V9ToV10(ctx, *dexkeeper)
	require.NoError(t, err)

	pair, found := dexkeeper.GetRegisteredPair(ctx, keepertest.TestContract, keepertest.TestPair.PriceDenom, keepertest.TestPair.AssetDenom)
	require.True(t, found)
	newTickSize := sdk.MustNewDecFromStr("0.0002")
	require.Equal(t, types.Pair{
		PriceDenom:       keepertest.TestPair.PriceDenom,
		AssetDenom:       keepertest.TestPair.AssetDenom,
		PriceTicksize:    &newTickSize,
		QuantityTicksize: &newTickSize,
	}, pair)

	// verify old/deprecated keeper store data is removed
	require.False(
		t,
		dexStore.Has(append(address, countBytes...)),
	)
	require.False(
		t,
		dexStore.Has(append([]byte(migrations.RegisteredPairCount), address...)),
	)
	require.False(
		t,
		dexStore.Has(append(append([]byte(migrations.PriceTickSizeKey), address...), pairPrefix...)),
	)
	require.False(
		t,
		dexStore.Has(append(append([]byte(migrations.QuantityTickSizeKey), address...), pairPrefix...)),
	)
}
