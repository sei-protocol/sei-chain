package rootmulti

import (
	"testing"

	"time"

	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/storev2/state"
	"github.com/sei-protocol/sei-db/config"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
)

func TestLastCommitID(t *testing.T) {
	store := NewStore(t.TempDir(), log.NewNopLogger(), config.StateCommitConfig{}, config.StateStoreConfig{}, false)
	require.Equal(t, types.CommitID{}, store.LastCommitID())
}

// waitUntilSSVersion waits until the SS latest version reaches at least target or times out.
func waitUntilSSVersion(t *testing.T, store *Store, target int64) {
	ss := store.GetStateStore()
	require.NotNil(t, ss)
	require.Eventually(t, func() bool {
		return ss.GetLatestVersion() >= target
	}, 10*time.Second, 10*time.Millisecond)
}

func TestSCSS_WriteAndHistoricalRead(t *testing.T) {
	// Enable both SC and SS with default configs (pebbledb backend, async writes)
	home := t.TempDir()
	scCfg := config.DefaultStateCommitConfig()
	scCfg.Enable = true
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = true

	store := NewStore(home, log.NewNopLogger(), scCfg, ssCfg, false)
	defer func() { _ = store.Close() }()

	// Mount one IAVL store and load
	key := types.NewKVStoreKey("store1")
	store.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
	require.NoError(t, store.LoadLatestVersion())

	// Write v1 and commit
	kv := store.GetStoreByName("store1").(types.KVStore)
	keyBytes := []byte("k")
	valV1 := []byte("v1")
	kv.Set(keyBytes, valV1)
	c1 := store.Commit(true)
	require.Equal(t, int64(1), c1.Version)

	// Re-acquire KV store after commit to ensure we write to the current instance
	kv = store.GetStoreByName("store1").(types.KVStore)
	// Write v2 and commit
	valV2 := []byte("v2")
	kv.Set(keyBytes, valV2)
	c2 := store.Commit(true)
	require.Equal(t, int64(2), c2.Version)

	// Wait for SS to asynchronously catch up to v2
	waitUntilSSVersion(t, store, c2.Version)

	// Current read (latest) should be v2
	cmsLatest, err := store.CacheMultiStoreWithVersion(c2.Version)
	require.NoError(t, err)
	gotLatest := cmsLatest.GetKVStore(key).Get(keyBytes)
	require.Equal(t, valV2, gotLatest)

	// Historical read at v1 should return v1 (served by SS)
	cmsV1, err := store.CacheMultiStoreWithVersion(c1.Version)
	require.NoError(t, err)
	gotV1 := cmsV1.GetKVStore(key).Get(keyBytes)
	require.Equal(t, valV1, gotV1)

	// Query API without proof at v1 should be served by SS and return v1
	resp := store.Query(abci.RequestQuery{
		Path:   "/store1/key",
		Data:   keyBytes,
		Height: c1.Version,
		Prove:  false,
	})
	require.EqualValues(t, 0, resp.Code)
	require.Equal(t, valV1, resp.Value)

	// Query API with proof at v1 should still return v1 (served by SC historical)
	resp = store.Query(abci.RequestQuery{
		Path:   "/store1/key",
		Data:   keyBytes,
		Height: c1.Version,
		Prove:  true,
	})
	require.EqualValues(t, 0, resp.Code)
	require.Equal(t, valV1, resp.Value)
}

// TestCacheMultiStoreWithVersion_OnlyUsesSSStores verifies that CacheMultiStoreWithVersion
// never adds SC stores and only adds SS stores for historical queries, ensuring that
// historical queries only serve from SS stores.
func TestCacheMultiStoreWithVersion_OnlyUsesSSStores(t *testing.T) {
	// Enable both SC and SS with default configs
	home := t.TempDir()
	scCfg := config.DefaultStateCommitConfig()
	scCfg.Enable = true
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = true

	store := NewStore(home, log.NewNopLogger(), scCfg, ssCfg, false)
	defer func() { _ = store.Close() }()

	// Mount IAVL stores and transient/mem stores
	iavlKey1 := types.NewKVStoreKey("iavl_store1")
	iavlKey2 := types.NewKVStoreKey("iavl_store2")
	transientKey := types.NewTransientStoreKey("transient_store")
	memKey := types.NewMemoryStoreKey("mem_store")

	store.MountStoreWithDB(iavlKey1, types.StoreTypeIAVL, nil)
	store.MountStoreWithDB(iavlKey2, types.StoreTypeIAVL, nil)
	store.MountStoreWithDB(transientKey, types.StoreTypeTransient, nil)
	store.MountStoreWithDB(memKey, types.StoreTypeMemory, nil)
	require.NoError(t, store.LoadLatestVersion())

	// Write data to IAVL stores and commit
	iavl1KV := store.GetStoreByName("iavl_store1").(types.KVStore)
	iavl2KV := store.GetStoreByName("iavl_store2").(types.KVStore)
	iavl1KV.Set([]byte("k1"), []byte("v1"))
	iavl2KV.Set([]byte("k2"), []byte("v2"))
	c1 := store.Commit(true)
	require.Equal(t, int64(1), c1.Version)

	// Write more data and commit again
	iavl1KV = store.GetStoreByName("iavl_store1").(types.KVStore)
	iavl2KV = store.GetStoreByName("iavl_store2").(types.KVStore)
	iavl1KV.Set([]byte("k1"), []byte("v1_updated"))
	iavl2KV.Set([]byte("k2"), []byte("v2_updated"))
	c2 := store.Commit(true)
	require.Equal(t, int64(2), c2.Version)

	// Wait for SS to asynchronously catch up to v2
	waitUntilSSVersion(t, store, c2.Version)

	// Test: Call CacheMultiStoreWithVersion for historical version v1
	cmsV1, err := store.CacheMultiStoreWithVersion(c1.Version)
	require.NoError(t, err)

	// Verify IAVL stores are SS stores (StoreTypeSSStore), not SC stores (StoreTypeIAVL)
	iavl1Store := cmsV1.GetKVStore(iavlKey1)
	iavl2Store := cmsV1.GetKVStore(iavlKey2)
	require.NotNil(t, iavl1Store)
	require.NotNil(t, iavl2Store)

	// The stores are wrapped in cachekv, but GetStoreType() delegates to the underlying store
	require.Equal(t, types.StoreType(state.StoreTypeSSStore), iavl1Store.GetStoreType(),
		"IAVL store should be SS store (StoreTypeSSStore), not SC store (StoreTypeIAVL)")
	require.Equal(t, types.StoreType(state.StoreTypeSSStore), iavl2Store.GetStoreType(),
		"IAVL store should be SS store (StoreTypeSSStore), not SC store (StoreTypeIAVL)")

	// Verify transient and mem stores are still present with their original types
	transientStore := cmsV1.GetKVStore(transientKey)
	memStore := cmsV1.GetKVStore(memKey)
	require.NotNil(t, transientStore)
	require.NotNil(t, memStore)
	require.Equal(t, types.StoreTypeTransient, transientStore.GetStoreType(),
		"Transient store should maintain its type")
	require.Equal(t, types.StoreTypeMemory, memStore.GetStoreType(),
		"Memory store should maintain its type")

	// Verify the stores serve historical data from SS (v1 values, not v2)
	require.Equal(t, []byte("v1"), iavl1Store.Get([]byte("k1")),
		"Should serve v1 data from SS store")
	require.Equal(t, []byte("v2"), iavl2Store.Get([]byte("k2")),
		"Should serve v1 data from SS store")

	// Verify that SC stores (StoreTypeIAVL) are NOT present in the returned cache multi store
	// by checking that all IAVL stores have StoreTypeSSStore, not StoreTypeIAVL
	require.NotEqual(t, types.StoreTypeIAVL, iavl1Store.GetStoreType(),
		"IAVL store should NOT be SC store (StoreTypeIAVL)")
	require.NotEqual(t, types.StoreTypeIAVL, iavl2Store.GetStoreType(),
		"IAVL store should NOT be SC store (StoreTypeIAVL)")

	// Test with latest version as well - should still use SS stores if available
	cmsV2, err := store.CacheMultiStoreWithVersion(c2.Version)
	require.NoError(t, err)
	iavl1StoreV2 := cmsV2.GetKVStore(iavlKey1)
	iavl2StoreV2 := cmsV2.GetKVStore(iavlKey2)
	require.Equal(t, types.StoreType(state.StoreTypeSSStore), iavl1StoreV2.GetStoreType(),
		"Latest version IAVL store should also be SS store")
	require.Equal(t, types.StoreType(state.StoreTypeSSStore), iavl2StoreV2.GetStoreType(),
		"Latest version IAVL store should also be SS store")
	require.Equal(t, []byte("v1_updated"), iavl1StoreV2.Get([]byte("k1")),
		"Should serve v2 data from SS store")
	require.Equal(t, []byte("v2_updated"), iavl2StoreV2.Get([]byte("k2")),
		"Should serve v2 data from SS store")
}
