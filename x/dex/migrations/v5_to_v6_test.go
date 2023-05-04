package migrations_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/store"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/migrations"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmdb "github.com/tendermint/tm-db"
)

func TestMigrate5to6(t *testing.T) {
	storeKey := sdk.NewKVStoreKey(types.StoreKey)
	memStoreKey := storetypes.NewMemoryStoreKey(types.MemStoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, sdk.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, sdk.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)
	ctx := sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())
	store := prefix.NewStore(
		ctx.KVStore(storeKey),
		[]byte(keeper.ContractPrefixKey),
	)

	oldContractA := types.LegacyContractInfo{
		CodeId:            1,
		ContractAddr:      "abc",
		NeedHook:          true,
		NeedOrderMatching: false,
	}
	oldContractB := types.LegacyContractInfo{
		CodeId:            2,
		ContractAddr:      "def",
		NeedHook:          false,
		NeedOrderMatching: true,
	}
	bzA, _ := oldContractA.Marshal()
	bzB, _ := oldContractB.Marshal()
	store.Set([]byte("abc"), bzA)
	store.Set([]byte("def"), bzB)

	err := migrations.V5ToV6(ctx, storeKey, cdc)
	require.Nil(t, err)

	newBzA := store.Get([]byte("abc"))
	newBzB := store.Get([]byte("def"))
	newContractA := types.ContractInfo{}
	newContractB := types.ContractInfo{}
	err = newContractA.Unmarshal(newBzA)
	require.Nil(t, err)
	err = newContractB.Unmarshal(newBzB)
	require.Nil(t, err)
	require.Equal(t, types.ContractInfo{
		CodeId:            1,
		ContractAddr:      "abc",
		NeedHook:          true,
		NeedOrderMatching: false,
	}, newContractA)
	require.Equal(t, types.ContractInfo{
		CodeId:            2,
		ContractAddr:      "def",
		NeedHook:          false,
		NeedOrderMatching: true,
	}, newContractB)
}
