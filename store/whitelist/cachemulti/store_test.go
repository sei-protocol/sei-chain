package cachemulti_test

import (
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/store"
	"github.com/sei-protocol/sei-chain/store/whitelist/cachemulti"
	"github.com/stretchr/testify/require"
)

var (
	WhitelistedStoreKey          = storetypes.NewKVStoreKey("whitelisted")
	NotWhitelistedStoreKey       = storetypes.NewKVStoreKey("not-whitelisted")
	TestStoreKeyToWriteWhitelist = map[string][]string{
		WhitelistedStoreKey.Name(): {"foo"},
	}
)

func TestWhitelistEnabledStore(t *testing.T) {
	stores := map[types.StoreKey]types.CacheWrapper{
		WhitelistedStoreKey: store.NewTestKVStore(),
	}
	multistore := store.NewTestCacheMultiStore(stores)
	whitelistMultistore := cachemulti.NewStore(multistore, TestStoreKeyToWriteWhitelist)
	kvStore := whitelistMultistore.GetKVStore(WhitelistedStoreKey)
	require.Panics(t, func() { kvStore.Delete([]byte("bar")) })
	require.NotPanics(t, func() { kvStore.Delete([]byte("foo")) })
}

func TestWhitelistDisabledStore(t *testing.T) {
	stores := map[types.StoreKey]types.CacheWrapper{
		WhitelistedStoreKey:    store.NewTestKVStore(),
		NotWhitelistedStoreKey: store.NewTestKVStore(),
	}
	multistore := store.NewTestCacheMultiStore(stores)
	whitelistMultistore := cachemulti.NewStore(multistore, TestStoreKeyToWriteWhitelist)
	kvStore := whitelistMultistore.GetKVStore(NotWhitelistedStoreKey)
	require.Panics(t, func() { kvStore.Delete([]byte("bar")) })
	require.Panics(t, func() { kvStore.Delete([]byte("foo")) })
}

func TestCacheStillWhitelist(t *testing.T) {
	stores := map[types.StoreKey]types.CacheWrapper{
		WhitelistedStoreKey:    store.NewTestKVStore(),
		NotWhitelistedStoreKey: store.NewTestKVStore(),
	}
	multistore := store.NewTestCacheMultiStore(stores)
	whitelistMultistore := cachemulti.NewStore(multistore, TestStoreKeyToWriteWhitelist)
	cacheWhitelistMultistore := whitelistMultistore.CacheMultiStore()
	kvStore := cacheWhitelistMultistore.GetKVStore(WhitelistedStoreKey)
	require.Panics(t, func() { kvStore.Delete([]byte("bar")) })
	require.NotPanics(t, func() { kvStore.Delete([]byte("foo")) })
}
