package keeper

import (
	"strings"
	"testing"

	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/store"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	typesparams "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmdb "github.com/tendermint/tm-db"
)

func TestMigrate2to3(t *testing.T) {
	storeKey := sdk.NewKVStoreKey(types.StoreKey)
	bankstorekey := sdk.NewKVStoreKey(banktypes.StoreKey)
	memStoreKey := storetypes.NewMemoryStoreKey(types.MemStoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, sdk.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(bankstorekey, sdk.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, sdk.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	paramsSubspace := typesparams.NewSubspace(cdc,
		codec.NewLegacyAmino(),
		storeKey,
		memStoreKey,
		"TokenfactoryParams",
	)
	oldCreateDenomFeeWhitelistKey := "createdenomfeewhitelist"
	KeySeparator := "|"

	// keys from old denom creation fee whitelist
	oldCreateDenomFeeWhitelistPrefix := []byte(strings.Join([]string{oldCreateDenomFeeWhitelistKey, ""}, KeySeparator))
	oldCreatorSpecificPrefix := []byte(strings.Join([]string{oldCreateDenomFeeWhitelistKey, "creator", ""}, KeySeparator))

	ctx := sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())
	if !paramsSubspace.HasKeyTable() {
		paramsSubspace = paramsSubspace.WithKeyTable(types.ParamKeyTable())
	}

	// Set dummy values for old denom creation whitelist
	store := ctx.KVStore(storeKey)
	store.Set(oldCreateDenomFeeWhitelistPrefix, []byte("garbage value whitelist"))
	store.Set(oldCreatorSpecificPrefix, []byte("garbage value whitelist creator"))
	require.True(t, store.Has(oldCreateDenomFeeWhitelistPrefix))
	require.True(t, store.Has(oldCreatorSpecificPrefix))
	newKeeper := NewKeeper(cdc, storeKey, paramsSubspace, nil, bankkeeper.NewBaseKeeper(cdc, bankstorekey, nil, paramsSubspace, nil), nil)
	m := NewMigrator(newKeeper)
	err := m.Migrate2to3(ctx)
	require.Nil(t, err)
	require.False(t, store.Has(oldCreateDenomFeeWhitelistPrefix))
	require.False(t, store.Has(oldCreatorSpecificPrefix))

	// Params should also be empty
	params := types.Params{}
	paramsSubspace.GetParamSet(ctx, &params)
	require.Equal(t, types.Params{}, params)

	m.keeper.addDenomFromCreator(ctx, "creator", "test_denom")
	m.keeper.bankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{Base: "test_denom", Name: "test_denom", Symbol: "test_denom"})
	err = m.Migrate3to4(ctx)
	require.Nil(t, err)
}

func TestMigrate3To4(t *testing.T) {
	// Test migration with all metadata denom
	metadata := banktypes.Metadata{Description: sdk.DefaultBondDenom, Base: sdk.DefaultBondDenom, Display: sdk.DefaultBondDenom, Name: sdk.DefaultBondDenom, Symbol: sdk.DefaultBondDenom}
	_, keeper := getStoreAndKeeper(t)
	m := NewMigrator(keeper)
	m.SetMetadata(&metadata)
	require.Equal(t, sdk.DefaultBondDenom, metadata.Display)
	require.Equal(t, sdk.DefaultBondDenom, metadata.Name)
	require.Equal(t, sdk.DefaultBondDenom, metadata.Symbol)
	// Test migration with missing fields
	testDenom := "test_denom"
	metadata = banktypes.Metadata{Base: testDenom}
	m.SetMetadata(&metadata)
	require.Equal(t, testDenom, metadata.Display)
	require.Equal(t, testDenom, metadata.Name)
	require.Equal(t, testDenom, metadata.Symbol)
}

func TestMigrate4To5(t *testing.T) {
	stateStore, keeper := getStoreAndKeeper(t)
	m := NewMigrator(keeper)
	ctx := sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())
	err := m.Migrate4to5(ctx)
	require.NoError(t, err)
	require.NotPanics(t, func() { m.keeper.GetParams(ctx) })
	params := m.keeper.GetParams(ctx)
	require.Equal(t, types.DefaultParams(), params)
}

func getStoreAndKeeper(t *testing.T) (store.CommitMultiStore, Keeper) {
	storeKey := sdk.NewKVStoreKey(types.StoreKey)
	bankStoreKey := sdk.NewKVStoreKey(banktypes.StoreKey)
	memStoreKey := storetypes.NewMemoryStoreKey(types.MemStoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, sdk.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(bankStoreKey, sdk.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, sdk.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	paramsSubspace := typesparams.NewSubspace(cdc,
		codec.NewLegacyAmino(),
		storeKey,
		memStoreKey,
		"TokenfactoryParams",
	)
	return stateStore, NewKeeper(nil, nil, paramsSubspace, nil, nil, nil)
}
