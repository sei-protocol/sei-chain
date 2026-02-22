package rootmulti

import (
	ics23 "github.com/confio/ics23/go"
	"github.com/cosmos/cosmos-sdk/storev2/state"
	"testing"

	"time"

	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-db/config"
	dbproto "github.com/sei-protocol/sei-db/proto"
	sctypes "github.com/sei-protocol/sei-db/sc/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
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

func TestQuery_ProofWithOversizedHeight_DoesNotPanic(t *testing.T) {
	tree := &mockQueryTree{
		version: 10,
		values:  map[string][]byte{"k": []byte("v")},
	}
	committer := &mockQueryCommitter{
		version: 10,
		tree:    tree,
		lastInfo: &dbproto.CommitInfo{
			Version: 10,
			StoreInfos: []dbproto.StoreInfo{
				{
					Name: "store1",
					CommitId: dbproto.CommitID{
						Version: 10,
						Hash:    []byte{0x01},
					},
				},
			},
		},
	}
	store := &Store{
		logger:         log.NewNopLogger(),
		scStore:        committer,
		lastCommitInfo: &types.CommitInfo{Version: 10},
		storesParams:   map[types.StoreKey]storeParams{},
	}

	keyBytes := []byte("k")
	var resp abci.ResponseQuery
	require.NotPanics(t, func() {
		resp = store.Query(abci.RequestQuery{
			Path:   "/store1/key",
			Data:   keyBytes,
			Height: 9999,
			Prove:  true,
		})
	})

	require.EqualValues(t, 0, resp.Code)
	require.Equal(t, []byte("v"), resp.Value)
	require.NotNil(t, resp.ProofOps)
	require.NotEmpty(t, resp.ProofOps.Ops)
}

type mockQueryTree struct {
	version int64
	values  map[string][]byte
}

func (m *mockQueryTree) Get(key []byte) []byte                 { return m.values[string(key)] }
func (m *mockQueryTree) Has(key []byte) bool                   { _, ok := m.values[string(key)]; return ok }
func (m *mockQueryTree) Set(key, value []byte)                 { m.values[string(key)] = value }
func (m *mockQueryTree) Remove(key []byte)                     { delete(m.values, string(key)) }
func (m *mockQueryTree) Version() int64                        { return m.version }
func (m *mockQueryTree) RootHash() []byte                      { return []byte{0xAB} }
func (m *mockQueryTree) Iterator(_, _ []byte, _ bool) dbm.Iterator { return nil }
func (m *mockQueryTree) GetProof(_ []byte) *ics23.CommitmentProof {
	return &ics23.CommitmentProof{}
}
func (m *mockQueryTree) Close() error { return nil }

type mockQueryCommitter struct {
	version  int64
	tree     sctypes.Tree
	lastInfo *dbproto.CommitInfo
}

func (m *mockQueryCommitter) Initialize(_ []string)                                 {}
func (m *mockQueryCommitter) Commit() (int64, error)                                { return m.version, nil }
func (m *mockQueryCommitter) Version() int64                                         { return m.version }
func (m *mockQueryCommitter) GetLatestVersion() (int64, error)                       { return m.version, nil }
func (m *mockQueryCommitter) GetEarliestVersion() (int64, error)                     { return 1, nil }
func (m *mockQueryCommitter) ApplyChangeSets(_ []*dbproto.NamedChangeSet) error      { return nil }
func (m *mockQueryCommitter) ApplyUpgrades(_ []*dbproto.TreeNameUpgrade) error       { return nil }
func (m *mockQueryCommitter) WorkingCommitInfo() *dbproto.CommitInfo                 { return m.lastInfo }
func (m *mockQueryCommitter) LastCommitInfo() *dbproto.CommitInfo                    { return m.lastInfo }
func (m *mockQueryCommitter) LoadVersion(_ int64, _ bool) (sctypes.Committer, error) { return m, nil }
func (m *mockQueryCommitter) Rollback(_ int64) error                                 { return nil }
func (m *mockQueryCommitter) SetInitialVersion(_ int64) error                        { return nil }
func (m *mockQueryCommitter) GetTreeByName(name string) sctypes.Tree {
	if name == "store1" {
		return m.tree
	}
	return nil
}
func (m *mockQueryCommitter) Importer(_ int64) (sctypes.Importer, error) { return nil, nil }
func (m *mockQueryCommitter) Exporter(_ int64) (sctypes.Exporter, error) { return nil, nil }
func (m *mockQueryCommitter) Close() error                                { return nil }

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
