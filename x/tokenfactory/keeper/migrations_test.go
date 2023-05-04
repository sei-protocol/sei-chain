package keeper

import (
	"strings"
	"testing"

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
	memStoreKey := storetypes.NewMemoryStoreKey(types.MemStoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, sdk.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, sdk.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	paramsSubspace := typesparams.NewSubspace(cdc,
		types.Amino,
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
	newKeeper := NewKeeper(cdc, storeKey, paramsSubspace, nil, nil, nil)
	m := NewMigrator(newKeeper)
	err := m.Migrate2to3(ctx)
	require.Nil(t, err)
	require.False(t, store.Has(oldCreateDenomFeeWhitelistPrefix))
	require.False(t, store.Has(oldCreatorSpecificPrefix))

	// Params should also be empty
	params := types.Params{}
	paramsSubspace.GetParamSet(ctx, &params)
	require.Equal(t, types.Params{}, params)
}
