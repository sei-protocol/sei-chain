package rootmulti

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/storev2/state"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	"golang.org/x/time/rate"
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

	// Occupy the historical-proof semaphore. No-proof + SS queries should bypass it.
	store.histProofSem <- struct{}{}

	// Query API without proof at v1 should be served by SS and return v1
	resp := store.Query(abci.RequestQuery{
		Path:   "/store1/key",
		Data:   keyBytes,
		Height: c1.Version,
		Prove:  false,
	})
	require.EqualValues(t, 0, resp.Code)
	require.Equal(t, valV1, resp.Value)

	<-store.histProofSem

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
// serves SS stores when enabled, and falls back to SC when SS is disabled, for
// height=0 (latest) and explicit latest height.
func TestCacheMultiStoreWithVersion_OnlyUsesSSStores(t *testing.T) {
	testCases := []struct {
		name      string
		ssEnabled bool
	}{
		{"ss-enabled", true},
		{"ss-disabled", false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			scCfg := config.DefaultStateCommitConfig()
			scCfg.Enable = true
			scCfg.AsyncCommitBuffer = 0
			ssCfg := config.DefaultStateStoreConfig()
			ssCfg.Enable = tc.ssEnabled
			ssCfg.AsyncWriteBuffer = 0

			store := NewStore(home, log.NewNopLogger(), scCfg, ssCfg, false)
			defer func() { _ = store.Close() }()

			iavlKey1 := types.NewKVStoreKey("iavl_store1")
			iavlKey2 := types.NewKVStoreKey("iavl_store2")
			transientKey := types.NewTransientStoreKey("transient_store")
			memKey := types.NewMemoryStoreKey("mem_store")

			store.MountStoreWithDB(iavlKey1, types.StoreTypeIAVL, nil)
			store.MountStoreWithDB(iavlKey2, types.StoreTypeIAVL, nil)
			store.MountStoreWithDB(transientKey, types.StoreTypeTransient, nil)
			store.MountStoreWithDB(memKey, types.StoreTypeMemory, nil)
			require.NoError(t, store.LoadLatestVersion())

			iavl1KV := store.GetStoreByName("iavl_store1").(types.KVStore)
			iavl2KV := store.GetStoreByName("iavl_store2").(types.KVStore)
			iavl1KV.Set([]byte("k1"), []byte("v1"))
			iavl2KV.Set([]byte("k2"), []byte("v2"))
			c1 := store.Commit(true)
			require.Equal(t, int64(1), c1.Version)

			iavl1KV = store.GetStoreByName("iavl_store1").(types.KVStore)
			iavl2KV = store.GetStoreByName("iavl_store2").(types.KVStore)
			iavl1KV.Set([]byte("k1"), []byte("v1_updated"))
			iavl2KV.Set([]byte("k2"), []byte("v2_updated"))
			c2 := store.Commit(true)
			require.Equal(t, int64(2), c2.Version)

			if tc.ssEnabled {
				waitUntilSSVersion(t, store, c2.Version)
			}

			queryVersions := []int64{0, c2.Version}
			for _, v := range queryVersions {
				cms, err := store.CacheMultiStoreWithVersion(v)
				require.NoError(t, err)

				iavl1Store := cms.GetKVStore(iavlKey1)
				iavl2Store := cms.GetKVStore(iavlKey2)
				require.NotNil(t, iavl1Store)
				require.NotNil(t, iavl2Store)

				if tc.ssEnabled {
					require.Equal(t, types.StoreType(state.StoreTypeSSStore), iavl1Store.GetStoreType())
					require.Equal(t, types.StoreType(state.StoreTypeSSStore), iavl2Store.GetStoreType())
				} else {
					require.Equal(t, types.StoreTypeIAVL, iavl1Store.GetStoreType())
					require.Equal(t, types.StoreTypeIAVL, iavl2Store.GetStoreType())
				}

				transientStore := cms.GetKVStore(transientKey)
				memStore := cms.GetKVStore(memKey)
				require.NotNil(t, transientStore)
				require.NotNil(t, memStore)
				require.Equal(t, types.StoreTypeTransient, transientStore.GetStoreType())
				require.Equal(t, types.StoreTypeMemory, memStore.GetStoreType())

				if v != 0 {
					require.Equal(t, []byte("v1_updated"), iavl1Store.Get([]byte("k1")))
					require.Equal(t, []byte("v2_updated"), iavl2Store.Get([]byte("k2")))
				}
			}

			if !tc.ssEnabled {
				cmsHistorical, err := store.CacheMultiStoreWithVersion(c1.Version)
				require.NoError(t, err)
				require.Panics(t, func() { _ = cmsHistorical.GetKVStore(iavlKey1) })
				require.Panics(t, func() { _ = cmsHistorical.GetKVStore(iavlKey2) })
			}
		})
	}
}

func TestTryAcquireHistProofPermit(t *testing.T) {
	t.Run("busy-when-semaphore-full", func(t *testing.T) {
		store := &Store{
			histProofSem: make(chan struct{}, 1),
		}

		require.NoError(t, store.tryAcquireHistProofPermit())

		err := store.tryAcquireHistProofPermit()
		require.Error(t, err)
		require.Contains(t, err.Error(), "historical proof busy")

		store.releaseHistProofPermit()
		store.releaseHistProofPermit() // no-op when empty
		require.NoError(t, store.tryAcquireHistProofPermit())
	})

	t.Run("rate-limited-before-semaphore-check", func(t *testing.T) {
		store := &Store{
			histProofSem:     make(chan struct{}, 2),
			histProofLimiter: rate.NewLimiter(rate.Limit(0.001), 1),
		}

		require.NoError(t, store.tryAcquireHistProofPermit())

		err := store.tryAcquireHistProofPermit()
		require.Error(t, err)
		require.Contains(t, err.Error(), "historical proof rate limited")
	})
}

func TestQuery_HistoricalNoProofWithoutSS_UsesPermit(t *testing.T) {
	home := t.TempDir()
	scCfg := config.DefaultStateCommitConfig()
	scCfg.Enable = true
	scCfg.HistoricalProofRateLimit = 0
	scCfg.HistoricalProofMaxInFlight = 1
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = false

	store := NewStore(home, log.NewNopLogger(), scCfg, ssCfg, false)
	defer func() { _ = store.Close() }()

	key := types.NewKVStoreKey("store1")
	store.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
	require.NoError(t, store.LoadLatestVersion())

	keyBytes := []byte("k")
	kv := store.GetStoreByName("store1").(types.KVStore)
	kv.Set(keyBytes, []byte("v1"))
	c1 := store.Commit(true)
	require.Equal(t, int64(1), c1.Version)

	kv = store.GetStoreByName("store1").(types.KVStore)
	kv.Set(keyBytes, []byte("v2"))
	c2 := store.Commit(true)
	require.Equal(t, int64(2), c2.Version)

	// Saturate historical permit and verify historical query is rejected.
	store.histProofSem <- struct{}{}
	defer func() { <-store.histProofSem }()

	resp := store.Query(abci.RequestQuery{
		Path:   "/store1/key",
		Data:   keyBytes,
		Height: c1.Version,
		Prove:  false,
	})
	require.NotEqualValues(t, 0, resp.Code)
	require.Contains(t, resp.Log, "historical proof busy")
}

func TestQuery_LatestProofBypassesHistoricalPermit(t *testing.T) {
	home := t.TempDir()
	scCfg := config.DefaultStateCommitConfig()
	scCfg.Enable = true
	scCfg.HistoricalProofRateLimit = 0
	scCfg.HistoricalProofMaxInFlight = 1
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = false

	store := NewStore(home, log.NewNopLogger(), scCfg, ssCfg, false)
	defer func() { _ = store.Close() }()

	key := types.NewKVStoreKey("store1")
	store.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
	require.NoError(t, store.LoadLatestVersion())

	keyBytes := []byte("k")
	valV1 := []byte("v1")
	kv := store.GetStoreByName("store1").(types.KVStore)
	kv.Set(keyBytes, valV1)
	c1 := store.Commit(true)
	require.Equal(t, int64(1), c1.Version)

	// Saturate permit; latest proof query should not need historical permit.
	store.histProofSem <- struct{}{}
	defer func() { <-store.histProofSem }()

	resp := store.Query(abci.RequestQuery{
		Path:   "/store1/key",
		Data:   keyBytes,
		Height: c1.Version,
		Prove:  true,
	})
	require.EqualValues(t, 0, resp.Code)
	require.Equal(t, valV1, resp.Value)
}
