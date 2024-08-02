package migrations_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/migrations"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/utils"
	"github.com/stretchr/testify/require"
)

func TestMigrate13to14(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	// write old params
	prevParams := types.Params{
		PriceSnapshotRetention:    1,
		SudoCallGasPrice:          sdk.OneDec(),
		BeginBlockGasLimit:        1,
		EndBlockGasLimit:          1,
		DefaultGasPerOrder:        1,
		DefaultGasPerCancel:       1,
		GasAllowancePerSettlement: 1,
		MinProcessableRent:        1,
	}
	dexkeeper.SetParams(ctx, prevParams)

	dexkeeper.SetContract(ctx, &types.ContractInfoV2{ContractAddr: keepertest.TestContract})
	longStore := prefix.NewStore(ctx.KVStore(dexkeeper.GetStoreKey()), types.OrderBookPrefix(
		true, keepertest.TestContract, keepertest.TestPair.PriceDenom, keepertest.TestPair.AssetDenom,
	))
	longEntry := types.LongBook{
		Price: sdk.MustNewDecFromStr("10.123"),
		Entry: &types.OrderEntry{
			Price:      sdk.MustNewDecFromStr("10.123"),
			Quantity:   sdk.MustNewDecFromStr("5"),
			PriceDenom: keepertest.TestPair.PriceDenom,
			AssetDenom: keepertest.TestPair.AssetDenom,
		},
	}
	longEntryKey, err := longEntry.GetPrice().Marshal()
	require.Nil(t, err)
	longStore.Set(longEntryKey, dexkeeper.Cdc.MustMarshal(&longEntry))

	shortStore := prefix.NewStore(ctx.KVStore(dexkeeper.GetStoreKey()), types.OrderBookPrefix(
		false, keepertest.TestContract, keepertest.TestPair.PriceDenom, keepertest.TestPair.AssetDenom,
	))
	shortEntry := types.ShortBook{
		Price: sdk.MustNewDecFromStr("12.456"),
		Entry: &types.OrderEntry{
			Price:      sdk.MustNewDecFromStr("12.456"),
			Quantity:   sdk.MustNewDecFromStr("4"),
			PriceDenom: keepertest.TestPair.PriceDenom,
			AssetDenom: keepertest.TestPair.AssetDenom,
		},
	}
	shortEntryKey, err := shortEntry.GetPrice().Marshal()
	require.Nil(t, err)
	shortStore.Set(shortEntryKey, dexkeeper.Cdc.MustMarshal(&shortEntry))

	// migrate to default params
	err = migrations.V13ToV14(ctx, *dexkeeper)
	require.NoError(t, err)
	params := dexkeeper.GetParams(ctx)
	require.Equal(t, params.OrderBookEntriesPerLoad, uint64(types.DefaultOrderBookEntriesPerLoad))

	require.Nil(t, longStore.Get(longEntryKey))
	require.Nil(t, shortStore.Get(shortEntryKey))
	var loadedLongEntry types.LongBook
	dexkeeper.Cdc.MustUnmarshal(longStore.Get(utils.DecToBigEndian(longEntry.Price)), &loadedLongEntry)
	require.Equal(t, longEntry.Price, loadedLongEntry.Price)
	require.Equal(t, *longEntry.Entry, *loadedLongEntry.Entry)
	var loadedShortEntry types.ShortBook
	dexkeeper.Cdc.MustUnmarshal(shortStore.Get(utils.DecToBigEndian(shortEntry.Price)), &loadedShortEntry)
	require.Equal(t, shortEntry.Price, loadedShortEntry.Price)
	require.Equal(t, *shortEntry.Entry, *loadedShortEntry.Entry)
}
