package rootmulti

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/mem"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/storev2/state"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	sscomposite "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestGetCommitKVStore_ReaderRespectsWriteLock(t *testing.T) {
	store := &Store{
		storeKeys: map[string]types.StoreKey{},
		ckvStores: map[types.StoreKey]types.CommitKVStore{},
	}
	key := types.NewKVStoreKey("bank")
	store.storeKeys[key.Name()] = key
	store.ckvStores[key] = mem.NewStore()

	store.mtx.Lock()

	readDone := make(chan types.CommitKVStore, 1)
	go func() {
		readDone <- store.GetCommitKVStore(key) //GetCommitKVStore is blocked until store.mtx is unlocked
	}()

	select {
	case <-readDone:
		t.Fatal("GetCommitKVStore returned while write lock held — RLock missing")
	case <-time.After(50 * time.Millisecond):
	}

	newVal := mem.NewStore()
	store.ckvStores = map[types.StoreKey]types.CommitKVStore{key: newVal}
	store.mtx.Unlock()

	require.Same(t, newVal, <-readDone)
}

func TestLastCommitID(t *testing.T) {
	store := NewStore(t.TempDir(), config.DefaultStateCommitConfig(), config.StateStoreConfig{}, []string{})
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
	// Enable both SC and SS, but make SC WAL writes synchronous so the
	// historical proof query below cannot race memIAVL durability.
	home := t.TempDir()
	scCfg := config.DefaultStateCommitConfig()
	scCfg.Enable = true
	scCfg.MemIAVLConfig.AsyncCommitBuffer = 0

	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = true

	store := NewStore(home, scCfg, ssCfg, []string{})
	defer func() { _ = store.Close() }()

	// Mount one IAVL store and load
	key := types.NewKVStoreKey("bank")
	store.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
	require.NoError(t, store.LoadLatestVersion())

	// Write v1 and commit
	kv := store.GetStoreByName("bank").(types.KVStore)
	keyBytes := []byte("k")
	valV1 := []byte("v1")
	kv.Set(keyBytes, valV1)
	c1 := store.Commit(true)
	require.Equal(t, int64(1), c1.Version)

	// Re-acquire KV store after commit to ensure we write to the current instance
	kv = store.GetStoreByName("bank").(types.KVStore)
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
	resp := store.Query(context.Background(), abci.RequestQuery{
		Path:   "/bank/key",
		Data:   keyBytes,
		Height: c1.Version,
		Prove:  false,
	})
	require.EqualValues(t, 0, resp.Code)
	require.Equal(t, valV1, resp.Value)

	<-store.histProofSem

	// Query API with proof at v1 should still return v1 (served by SC historical)
	resp = store.Query(context.Background(), abci.RequestQuery{
		Path:   "/bank/key",
		Data:   keyBytes,
		Height: c1.Version,
		Prove:  true,
	})
	require.EqualValues(t, 0, resp.Code)
	require.Equal(t, valV1, resp.Value)

	// Once SS reports a higher floor, historical SS queries below it must fail
	// before a cache store can mix available Cosmos data with unavailable routed
	// data.
	require.NoError(t, store.ssStore.SetEarliestVersion(c2.Version, false))
	_, err = store.CacheMultiStoreWithVersion(c1.Version)
	require.ErrorContains(t, err, "below earliest available version 2")

	resp = store.Query(context.Background(), abci.RequestQuery{
		Path:   "/bank/key",
		Data:   keyBytes,
		Height: c1.Version,
		Prove:  false,
	})
	require.NotEqualValues(t, 0, resp.Code)
}

// A boundary produces an SS snapshot whether or not the block carried changesets, which is what
// routing every committed block through CommitBlock buys.
func TestFlushSchedulesSSSnapshotAtABoundary(t *testing.T) {
	for _, tc := range []struct {
		name         string
		writeAtBlock int64
	}{
		{name: "boundary block is populated", writeAtBlock: 2},
		{name: "boundary block is empty", writeAtBlock: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			scCfg := config.DefaultStateCommitConfig()
			scCfg.Enable = true
			scCfg.MemIAVLConfig.AsyncCommitBuffer = 0
			// SS mirrors the SC cadence, so this is what puts the SS boundary at 2.
			scCfg.MemIAVLConfig.SnapshotInterval = 2
			scCfg.MemIAVLConfig.SnapshotKeepRecent = 1

			ssCfg := config.DefaultStateStoreConfig()
			ssCfg.Enable = true
			ssCfg.SnapshotEnable = true

			store := NewStore(home, scCfg, ssCfg, []string{})
			defer func() { _ = store.Close() }()
			require.NotNil(t, store.ssCommitter, "SS commit capability was not resolved")

			key := types.NewKVStoreKey("bank")
			store.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
			require.NoError(t, store.LoadLatestVersion())

			for block := int64(1); block <= 2; block++ {
				if block == tc.writeAtBlock {
					store.GetStoreByName("bank").(types.KVStore).Set([]byte("k"), []byte("v"))
				}
				require.Equal(t, block, store.Commit(true).Version)
			}

			root := filepath.Join(home, "data", "state_store", sscomposite.SnapshotsDirName)
			require.Eventually(t, func() bool {
				versions, err := sscomposite.ListSnapshotVersions(root)
				return err == nil && len(versions) == 1 && versions[0] == 2
			}, 10*time.Second, 20*time.Millisecond, "boundary did not produce an SS snapshot")
		})
	}
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
			ssCfg := config.DefaultStateStoreConfig()
			ssCfg.Enable = tc.ssEnabled
			ssCfg.AsyncWriteBuffer = 0

			store := NewStore(home, scCfg, ssCfg, []string{})
			defer func() { _ = store.Close() }()

			iavlKey1 := types.NewKVStoreKey("bank")
			iavlKey2 := types.NewKVStoreKey("staking")
			transientKey := types.NewTransientStoreKey("transient_store")
			memKey := types.NewMemoryStoreKey("mem_store")

			store.MountStoreWithDB(iavlKey1, types.StoreTypeIAVL, nil)
			store.MountStoreWithDB(iavlKey2, types.StoreTypeIAVL, nil)
			store.MountStoreWithDB(transientKey, types.StoreTypeTransient, nil)
			store.MountStoreWithDB(memKey, types.StoreTypeMemory, nil)
			require.NoError(t, store.LoadLatestVersion())

			iavl1KV := store.GetStoreByName("bank").(types.KVStore)
			iavl2KV := store.GetStoreByName("staking").(types.KVStore)
			iavl1KV.Set([]byte("k1"), []byte("v1"))
			iavl2KV.Set([]byte("k2"), []byte("v2"))
			c1 := store.Commit(true)
			require.Equal(t, int64(1), c1.Version)

			iavl1KV = store.GetStoreByName("bank").(types.KVStore)
			iavl2KV = store.GetStoreByName("staking").(types.KVStore)
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
				_, err := store.CacheMultiStoreWithVersion(c1.Version)
				require.Error(t, err)
				require.Contains(t, err.Error(), fmt.Sprintf("unable to load historical state with SS disabled for version: %d", c1.Version))
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

	store := NewStore(home, scCfg, ssCfg, []string{})
	defer func() { _ = store.Close() }()

	key := types.NewKVStoreKey("bank")
	store.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
	require.NoError(t, store.LoadLatestVersion())

	keyBytes := []byte("k")
	kv := store.GetStoreByName("bank").(types.KVStore)
	kv.Set(keyBytes, []byte("v1"))
	c1 := store.Commit(true)
	require.Equal(t, int64(1), c1.Version)

	kv = store.GetStoreByName("bank").(types.KVStore)
	kv.Set(keyBytes, []byte("v2"))
	c2 := store.Commit(true)
	require.Equal(t, int64(2), c2.Version)

	// Saturate historical permit and verify historical query is rejected.
	store.histProofSem <- struct{}{}
	defer func() { <-store.histProofSem }()

	resp := store.Query(context.Background(), abci.RequestQuery{
		Path:   "/bank/key",
		Data:   keyBytes,
		Height: c1.Version,
		Prove:  false,
	})
	require.NotEqualValues(t, 0, resp.Code)
	require.Contains(t, resp.Log, "historical proof busy")
}

// TestCacheMultiStoreWithVersion_NoReentrantRLockDeadlock stress-tests that
// CacheMultiStoreWithVersion does not deadlock with concurrent writers.
//
// The deadlock scenario (with the old re-entrant RLock bug):
//  1. Reader goroutine calls CacheMultiStoreWithVersion, acquires RLock.
//  2. Writer goroutine calls Lock, blocks — and marks the RWMutex as writer-pending.
//  3. Reader calls CacheMultiStore which attempts a second RLock — this blocks
//     because Go's RWMutex starves new readers when a writer is pending.
//  4. Deadlock: reader holds RLock waiting for RLock, writer waits for reader's RLock.
//
// By racing many readers and writers concurrently we make it statistically
// near-certain that a writer queues between the two RLock calls (in buggy code).
func TestCacheMultiStoreWithVersion_NoReentrantRLockDeadlock(t *testing.T) {
	home := t.TempDir()
	scCfg := config.DefaultStateCommitConfig()
	scCfg.Enable = true
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = false

	store := NewStore(home, scCfg, ssCfg, []string{})
	defer func() { _ = store.Close() }()

	key := types.NewKVStoreKey("bank")
	store.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
	require.NoError(t, store.LoadLatestVersion())

	kv := store.GetStoreByName("bank").(types.KVStore)
	kv.Set([]byte("k"), []byte("v"))
	c1 := store.Commit(true)
	require.Equal(t, int64(1), c1.Version)

	const (
		numReaders = 8
		numWriters = 8
		duration   = 200 * time.Millisecond
	)

	stop := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// version=0 with SS disabled takes the else-if branch that
				// previously called CacheMultiStore (re-entrant RLock).
				cms, _ := store.CacheMultiStoreWithVersion(0)
				_ = cms
			}
		}()
	}

	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				//lint:ignore SA2001 intentional empty critical section to test lock contention
				store.mtx.Lock()
				store.mtx.Unlock()
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		time.Sleep(duration)
		close(stop)
		wg.Wait()
		close(done)
	}()

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 5*time.Second, 20*time.Millisecond,
		"CacheMultiStoreWithVersion deadlocked with concurrent writers")
}

func TestQuery_LatestProofBypassesHistoricalPermit(t *testing.T) {
	home := t.TempDir()
	scCfg := config.DefaultStateCommitConfig()
	scCfg.Enable = true
	scCfg.HistoricalProofRateLimit = 0
	scCfg.HistoricalProofMaxInFlight = 1
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = false

	store := NewStore(home, scCfg, ssCfg, []string{})
	defer func() { _ = store.Close() }()

	key := types.NewKVStoreKey("bank")
	store.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
	require.NoError(t, store.LoadLatestVersion())

	keyBytes := []byte("k")
	valV1 := []byte("v1")
	kv := store.GetStoreByName("bank").(types.KVStore)
	kv.Set(keyBytes, valV1)
	c1 := store.Commit(true)
	require.Equal(t, int64(1), c1.Version)

	// Saturate permit; latest proof query should not need historical permit.
	store.histProofSem <- struct{}{}
	defer func() { <-store.histProofSem }()

	resp := store.Query(context.Background(), abci.RequestQuery{
		Path:   "/bank/key",
		Data:   keyBytes,
		Height: c1.Version,
		Prove:  true,
	})
	require.EqualValues(t, 0, resp.Code)
	require.Equal(t, valV1, resp.Value)
}

func TestTryAcquireSubspaceQueryPermit(t *testing.T) {
	store := &Store{
		subspaceQuerySem: make(chan struct{}, 2),
	}

	require.NoError(t, store.tryAcquireSubspaceQueryPermit())
	require.NoError(t, store.tryAcquireSubspaceQueryPermit())

	err := store.tryAcquireSubspaceQueryPermit()
	require.Error(t, err)
	require.Contains(t, err.Error(), "subspace query busy")

	store.releaseSubspaceQueryPermit()
	store.releaseSubspaceQueryPermit()
	store.releaseSubspaceQueryPermit() // no-op when empty
	require.NoError(t, store.tryAcquireSubspaceQueryPermit())
}

func TestQuery_SubspaceSemaphoreRejectsWhenSaturated(t *testing.T) {
	home := t.TempDir()
	scCfg := config.DefaultStateCommitConfig()
	scCfg.Enable = true
	scCfg.MemIAVLConfig.AsyncCommitBuffer = 0
	scCfg.SubspaceQueryMaxInFlight = 2

	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = true

	store := NewStore(home, scCfg, ssCfg, []string{})
	defer func() { _ = store.Close() }()

	key := types.NewKVStoreKey("bank")
	store.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
	require.NoError(t, store.LoadLatestVersion())

	kv := store.GetStoreByName("bank").(types.KVStore)
	kv.Set([]byte("ab1"), []byte("v1"))
	require.Equal(t, int64(1), store.Commit(true).Version)
	waitUntilSSVersion(t, store, 1)

	store.subspaceQuerySem <- struct{}{}
	store.subspaceQuerySem <- struct{}{}
	defer func() {
		<-store.subspaceQuerySem
		<-store.subspaceQuerySem
	}()

	resp := store.Query(t.Context(), abci.RequestQuery{
		Path: "/bank/subspace",
		Data: []byte("ab"),
	})
	require.NotEqualValues(t, 0, resp.Code)
	require.Contains(t, resp.Log, "subspace query busy")
}

func TestQuery_SubspaceNarrowPrefixAndKeyUnaffected(t *testing.T) {
	home := t.TempDir()
	scCfg := config.DefaultStateCommitConfig()
	scCfg.Enable = true
	scCfg.MemIAVLConfig.AsyncCommitBuffer = 0

	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = true

	store := NewStore(home, scCfg, ssCfg, []string{})
	defer func() { _ = store.Close() }()

	key := types.NewKVStoreKey("bank")
	store.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
	require.NoError(t, store.LoadLatestVersion())

	keyBytes := []byte("ab1")
	kv := store.GetStoreByName("bank").(types.KVStore)
	kv.Set(keyBytes, []byte("v1"))
	kv.Set([]byte("ab2"), []byte("v2"))
	require.Equal(t, int64(1), store.Commit(true).Version)
	waitUntilSSVersion(t, store, 1)

	subspaceResp := store.Query(t.Context(), abci.RequestQuery{
		Path: "/bank/subspace",
		Data: []byte("ab"),
	})
	require.EqualValues(t, 0, subspaceResp.Code)
	require.NotEmpty(t, subspaceResp.Value)

	keyResp := store.Query(t.Context(), abci.RequestQuery{
		Path: "/bank/key",
		Data: keyBytes,
	})
	require.EqualValues(t, 0, keyResp.Code)
	require.Equal(t, []byte("v1"), keyResp.Value)
}

func TestQuery_SubspaceEmptyPrefixRejected(t *testing.T) {
	home := t.TempDir()
	scCfg := config.DefaultStateCommitConfig()
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.Enable = true

	store := NewStore(home, scCfg, ssCfg, []string{})
	defer func() { _ = store.Close() }()

	key := types.NewKVStoreKey("bank")
	store.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
	require.NoError(t, store.LoadLatestVersion())

	resp := store.Query(t.Context(), abci.RequestQuery{
		Path: "/bank/subspace",
	})
	require.NotEqualValues(t, 0, resp.Code)
	require.Contains(t, resp.Log, "subspace prefix must not be empty")
}
