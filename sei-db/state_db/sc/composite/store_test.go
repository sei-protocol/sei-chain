package composite

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

// failingEVMStore is a mock flatkv.Store whose LoadVersion always fails.
type failingEVMStore struct{}

var _ flatkv.Store = (*failingEVMStore)(nil)

func (f *failingEVMStore) LoadVersion(int64, bool) (flatkv.Store, error) {
	return nil, fmt.Errorf("flatkv unavailable")
}
func (f *failingEVMStore) ApplyChangeSets([]*proto.NamedChangeSet) error { return nil }
func (f *failingEVMStore) Commit() (int64, error)                        { return 0, nil }
func (f *failingEVMStore) Get([]byte) ([]byte, bool)                     { return nil, false }
func (f *failingEVMStore) Has([]byte) bool                               { return false }
func (f *failingEVMStore) Iterator(_, _ []byte) flatkv.Iterator          { return nil }
func (f *failingEVMStore) IteratorByPrefix([]byte) flatkv.Iterator       { return nil }
func (f *failingEVMStore) RootHash() []byte                              { return nil }
func (f *failingEVMStore) Version() int64                                { return 0 }
func (f *failingEVMStore) WriteSnapshot(string) error                    { return nil }
func (f *failingEVMStore) Rollback(int64) error                          { return nil }
func (f *failingEVMStore) Exporter(int64) (types.Exporter, error)        { return nil, nil }
func (f *failingEVMStore) Importer(int64) (types.Importer, error)        { return nil, nil }
func (f *failingEVMStore) GetPhaseTimer() *metrics.PhaseTimer            { return nil }
func (f *failingEVMStore) CommittedRootHash() []byte                     { return nil }
func (f *failingEVMStore) Close() error                                  { return nil }

func TestCompositeStoreBasicOperations(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test", EVMStoreName})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, cs.Close())
	}()

	require.Equal(t, int64(0), cs.Version())

	// Apply changesets with both regular and EVM data
	changesets := []*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key1"), Value: []byte("value1")},
				},
			},
		},
		{
			Name: EVMStoreName,
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("evm_key1"), Value: []byte("evm_value1")},
				},
			},
		},
	}
	err = cs.ApplyChangeSets(changesets)
	require.NoError(t, err)

	version, err := cs.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), version)
	require.Equal(t, int64(1), cs.Version())

	testStore := cs.GetChildStoreByName("test")
	require.NotNil(t, testStore)

	evmStore := cs.GetChildStoreByName(EVMStoreName)
	require.NotNil(t, evmStore)
}

func TestEmptyChangesets(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, cs.Close())
	}()

	// Empty changesets should be no-op
	err = cs.ApplyChangeSets(nil)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{})
	require.NoError(t, err)
}

func TestLoadVersionCopyExisting(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key"), Value: []byte("value")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)
	require.NoError(t, cs.Close())

	// Load with copyExisting=true
	newCS, err := cs.LoadVersion(0, true)
	require.NoError(t, err)
	require.NotNil(t, newCS)

	compositeCS, ok := newCS.(*CompositeCommitStore)
	require.True(t, ok)
	require.NotSame(t, cs, compositeCS)

	require.NoError(t, compositeCS.Close())
}

func TestWorkingAndLastCommitInfo(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, cs.Close())
	}()

	workingInfo := cs.WorkingCommitInfo()
	require.NotNil(t, workingInfo)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key"), Value: []byte("value")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	lastInfo := cs.LastCommitInfo()
	require.NotNil(t, lastInfo)
	require.Equal(t, int64(1), lastInfo.Version)
}

func TestLatticeHashCommitInfo(t *testing.T) {
	addr := [20]byte{0xAA}
	slot := [32]byte{0xBB}
	evmStorageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, append(addr[:], slot[:]...))

	makeChangesets := func(round byte) []*proto.NamedChangeSet {
		return []*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key"), Value: []byte{round}},
					},
				},
			},
			{
				Name: EVMStoreName,
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: evmStorageKey, Value: []byte{round}},
					},
				},
			},
		}
	}

	tests := []struct {
		name          string
		writeMode     config.WriteMode
		enableLattice bool
		expectLattice bool
	}{
		{"CosmosOnly/lattice_off", config.CosmosOnlyWrite, false, false},
		{"CosmosOnly/lattice_on", config.CosmosOnlyWrite, true, false},
		{"DualWrite/lattice_off", config.DualWrite, false, false},
		{"DualWrite/lattice_on", config.DualWrite, true, true},
		{"SplitWrite/lattice_on", config.SplitWrite, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.DefaultStateCommitConfig()
			cfg.WriteMode = tt.writeMode
			cfg.EnableLatticeHash = tt.enableLattice

			cs := NewCompositeCommitStore(t.Context(), dir, cfg)
			cs.Initialize([]string{"test", EVMStoreName})
			_, err := cs.LoadVersion(0, false)
			require.NoError(t, err)
			defer cs.Close()

			var prevLastHash []byte

			for round := byte(1); round <= 3; round++ {
				require.NoError(t, cs.ApplyChangeSets(makeChangesets(round)))

				// --- Working commit info ---
				expectedCosmos := cs.cosmosCommitter.WorkingCommitInfo()
				var expectedEvmHash []byte
				if tt.expectLattice {
					expectedEvmHash = cs.evmCommitter.RootHash()
				}

				workingInfo := cs.WorkingCommitInfo()
				cosmosCount := len(expectedCosmos.StoreInfos)
				if tt.expectLattice {
					require.Equal(t, cosmosCount+1, len(workingInfo.StoreInfos))
				} else {
					require.Equal(t, cosmosCount, len(workingInfo.StoreInfos))
				}
				for i, si := range expectedCosmos.StoreInfos {
					require.Equal(t, si.Name, workingInfo.StoreInfos[i].Name)
					require.Equal(t, si.CommitId.Hash, workingInfo.StoreInfos[i].CommitId.Hash)
				}
				if tt.expectLattice {
					entry := workingInfo.StoreInfos[len(workingInfo.StoreInfos)-1]
					require.Equal(t, "evm_lattice", entry.Name)
					require.Equal(t, expectedEvmHash, entry.CommitId.Hash)
					require.Equal(t, workingInfo.Version, entry.CommitId.Version)

					// Verify no duplicate names — important for app hash merkle tree
					names := make(map[string]int)
					for _, si := range workingInfo.StoreInfos {
						names[si.Name]++
					}
					for name, count := range names {
						require.Equal(t, 1, count, "duplicate store name %q in WorkingCommitInfo", name)
					}
				}

				// --- Commit ---
				_, err = cs.Commit()
				require.NoError(t, err)

				// --- Last commit info ---
				expectedCosmosLast := cs.cosmosCommitter.LastCommitInfo()
				var expectedEvmCommitted []byte
				if tt.expectLattice {
					expectedEvmCommitted = cs.evmCommitter.CommittedRootHash()
					require.Equal(t, expectedEvmHash, expectedEvmCommitted)
				}

				lastInfo := cs.LastCommitInfo()
				require.Equal(t, int64(round), lastInfo.Version)
				cosmosLastCount := len(expectedCosmosLast.StoreInfos)
				if tt.expectLattice {
					require.Equal(t, cosmosLastCount+1, len(lastInfo.StoreInfos))
				} else {
					require.Equal(t, cosmosLastCount, len(lastInfo.StoreInfos))
				}
				for i, si := range expectedCosmosLast.StoreInfos {
					require.Equal(t, si.Name, lastInfo.StoreInfos[i].Name)
					require.Equal(t, si.CommitId.Hash, lastInfo.StoreInfos[i].CommitId.Hash)
				}
				if tt.expectLattice {
					entry := lastInfo.StoreInfos[len(lastInfo.StoreInfos)-1]
					require.Equal(t, "evm_lattice", entry.Name)
					require.Equal(t, expectedEvmCommitted, entry.CommitId.Hash)
					require.Equal(t, lastInfo.Version, entry.CommitId.Version)

					// Verify no duplicate names — important for app hash merkle tree
					names := make(map[string]int)
					for _, si := range lastInfo.StoreInfos {
						names[si.Name]++
					}
					for name, count := range names {
						require.Equal(t, 1, count, "duplicate store name %q in LastCommitInfo", name)
					}

					// Hash must change between rounds since data differs
					if prevLastHash != nil {
						require.NotEqual(t, prevLastHash, entry.CommitId.Hash,
							"lattice hash should change across commits")
					}
					prevLastHash = entry.CommitId.Hash
				}
			}
		})
	}
}

func TestRollback(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit a few versions
	for i := 0; i < 3; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key"), Value: []byte("value" + string(rune('0'+i)))},
					},
				},
			},
		})
		require.NoError(t, err)
		_, err = cs.Commit()
		require.NoError(t, err)
	}

	require.Equal(t, int64(3), cs.Version())

	err = cs.Rollback(2)
	require.NoError(t, err)
	require.Equal(t, int64(2), cs.Version())

	require.NoError(t, cs.Close())
}

func TestGetVersions(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key"), Value: []byte("value")},
					},
				},
			},
		})
		require.NoError(t, err)
		_, err = cs.Commit()
		require.NoError(t, err)
	}
	require.NoError(t, cs.Close())

	cs2 := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs2.Initialize([]string{"test"})

	latestVersion, err := cs2.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(3), latestVersion)
}

func TestReadOnlyLoadVersionSoftFailsWhenFlatKVUnavailable(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key1"), Value: []byte("value1")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	// Inject a failing EVM committer to simulate FlatKV being unavailable
	// for historical versions (different retention, late enablement, etc).
	cs.evmCommitter = &failingEVMStore{}

	readOnly, err := cs.LoadVersion(0, true)
	require.NoError(t, err, "readonly LoadVersion should succeed even when FlatKV fails")
	defer func() { _ = readOnly.Close() }()

	compositeRO, ok := readOnly.(*CompositeCommitStore)
	require.True(t, ok)
	require.Nil(t, compositeRO.evmCommitter, "evmCommitter should be nil when FlatKV failed")

	// Cosmos data should still be accessible
	store := compositeRO.GetChildStoreByName("test")
	require.NotNil(t, store)
	val := store.Get([]byte("key1"))
	require.Equal(t, []byte("value1"), val)
}

// =============================================================================
// Export / Import Tests
// =============================================================================

// exportedItem stores one item produced by an exporter (module name or snapshot node).
type exportedItem struct {
	moduleName string
	node       *types.SnapshotNode
}

// drainCompositeExporter collects all items from an exporter in stream order.
func drainCompositeExporter(t *testing.T, exp types.Exporter) []exportedItem {
	t.Helper()
	var items []exportedItem
	for {
		raw, err := exp.Next()
		if err != nil {
			require.True(t, errors.Is(err, errorutils.ErrorExportDone), "unexpected error: %v", err)
			break
		}
		switch v := raw.(type) {
		case string:
			items = append(items, exportedItem{moduleName: v})
		case *types.SnapshotNode:
			items = append(items, exportedItem{node: v})
		default:
			t.Fatalf("unexpected item type %T", raw)
		}
	}
	return items
}

// replayImport feeds exported items into an importer.
func replayImport(t *testing.T, imp types.Importer, items []exportedItem) {
	t.Helper()
	for _, it := range items {
		if it.moduleName != "" {
			require.NoError(t, imp.AddModule(it.moduleName))
		} else {
			imp.AddNode(it.node)
		}
	}
}

// splitWriteConfig returns a StateCommitConfig with SplitWrite mode and
// fast snapshot intervals so that memiavl snapshots exist for the exporter.
func splitWriteConfig() config.StateCommitConfig {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = config.SplitWrite
	cfg.EnableLatticeHash = true
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	return cfg
}

func TestExportImportSplitWrite(t *testing.T) {
	cfg := splitWriteConfig()

	// --- Source store: write cosmos + EVM data ---
	srcDir := t.TempDir()
	src := NewCompositeCommitStore(t.Context(), srcDir, cfg)
	src.Initialize([]string{"bank", EVMStoreName})
	_, err := src.LoadVersion(0, false)
	require.NoError(t, err)

	addr := flatkv.Address{0xAA}
	slot := flatkv.Slot{0xBB}
	storageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage,
		flatkv.StorageKey(addr, slot))
	storageVal := []byte{0x42}

	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	nonceVal := []byte{0, 0, 0, 0, 0, 0, 0, 10}

	err = src.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: []byte("balance_alice"), Value: []byte("100")},
		}}},
		{Name: EVMStoreName, Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: storageKey, Value: storageVal},
			{Key: nonceKey, Value: nonceVal},
		}}},
	})
	require.NoError(t, err)
	_, err = src.Commit()
	require.NoError(t, err)

	// --- Export ---
	exporter, err := src.Exporter(1)
	require.NoError(t, err)
	items := drainCompositeExporter(t, exporter)
	require.NoError(t, exporter.Close())
	require.NoError(t, src.Close())

	// Verify export stream structure: cosmos modules first, evm_flatkv last.
	var moduleNames []string
	for _, it := range items {
		if it.moduleName != "" {
			moduleNames = append(moduleNames, it.moduleName)
		}
	}
	require.Contains(t, moduleNames, "bank")
	require.Contains(t, moduleNames, EVMFlatKVStoreName)
	// evm_flatkv should be the last module
	require.Equal(t, EVMFlatKVStoreName, moduleNames[len(moduleNames)-1])

	// --- Destination store: import ---
	dstDir := t.TempDir()
	dst := NewCompositeCommitStore(t.Context(), dstDir, cfg)
	dst.Initialize([]string{"bank", EVMStoreName})
	_, err = dst.LoadVersion(0, false)
	require.NoError(t, err)
	require.NoError(t, dst.Close())

	importer, err := dst.Importer(1)
	require.NoError(t, err)
	replayImport(t, importer, items)
	require.NoError(t, importer.Close())

	// Reload the store at version 1 to verify
	_, err = dst.LoadVersion(1, false)
	require.NoError(t, err)
	defer dst.Close()

	// Verify cosmos data
	bankStore := dst.GetChildStoreByName("bank")
	require.NotNil(t, bankStore)
	require.Equal(t, []byte("100"), bankStore.Get([]byte("balance_alice")))

	// Verify FlatKV data
	require.NotNil(t, dst.evmCommitter)
	got, found := dst.evmCommitter.Get(storageKey)
	require.True(t, found, "storage key should exist in FlatKV after import")
	require.Equal(t, storageVal, got)

	got, found = dst.evmCommitter.Get(nonceKey)
	require.True(t, found, "nonce key should exist in FlatKV after import")
	require.Equal(t, nonceVal, got)
}

func TestExportCosmosOnlyHasNoFlatKVModule(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0

	dir := t.TempDir()
	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"bank"})
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "bank", Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: []byte("key1"), Value: []byte("val1")},
		}}},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	exporter, err := cs.Exporter(1)
	require.NoError(t, err)
	items := drainCompositeExporter(t, exporter)
	require.NoError(t, exporter.Close())
	require.NoError(t, cs.Close())

	// In cosmos_only mode, evm_flatkv should NOT appear
	for _, it := range items {
		require.NotEqual(t, EVMFlatKVStoreName, it.moduleName,
			"evm_flatkv should not appear in cosmos_only export")
	}
}

func TestCompositeImporterRouting(t *testing.T) {
	// Verify that the composite importer routes evm_flatkv exclusively
	// to the evm importer and other modules only to cosmos.
	var cosmosModules, evmModules []string
	var cosmosNodes, evmNodes []*types.SnapshotNode

	cosmosImp := &trackingImporter{
		modules: &cosmosModules,
		nodes:   &cosmosNodes,
	}
	evmImp := &trackingImporter{
		modules: &evmModules,
		nodes:   &evmNodes,
	}

	imp := NewImporter(cosmosImp, evmImp)

	require.NoError(t, imp.AddModule("bank"))
	imp.AddNode(&types.SnapshotNode{Key: []byte("k1"), Value: []byte("v1")})

	require.NoError(t, imp.AddModule(EVMFlatKVStoreName))
	imp.AddNode(&types.SnapshotNode{Key: []byte("k2"), Value: []byte("v2")})

	require.NoError(t, imp.AddModule("staking"))
	imp.AddNode(&types.SnapshotNode{Key: []byte("k3"), Value: []byte("v3")})

	// bank and staking → cosmos only
	require.Equal(t, []string{"bank", "staking"}, cosmosModules)
	require.Len(t, cosmosNodes, 2)
	require.Equal(t, []byte("k1"), cosmosNodes[0].Key)
	require.Equal(t, []byte("k3"), cosmosNodes[1].Key)

	// evm_flatkv → evm only
	require.Equal(t, []string{EVMFlatKVStoreName}, evmModules)
	require.Len(t, evmNodes, 1)
	require.Equal(t, []byte("k2"), evmNodes[0].Key)

	require.NoError(t, imp.Close())
}

// trackingImporter records calls for test assertions.
type trackingImporter struct {
	modules *[]string
	nodes   *[]*types.SnapshotNode
}

func (ti *trackingImporter) AddModule(name string) error {
	*ti.modules = append(*ti.modules, name)
	return nil
}

func (ti *trackingImporter) AddNode(node *types.SnapshotNode) {
	*ti.nodes = append(*ti.nodes, node)
}

func (ti *trackingImporter) Close() error { return nil }

func TestReconcileVersionsAfterCrash(t *testing.T) {
	addr := [20]byte{0xAA}
	slot := [32]byte{0xBB}
	storageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage,
		flatkv.StorageKey(addr, slot))

	cfg := splitWriteConfig()

	dir := t.TempDir()
	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test", EVMStoreName})
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	for i := byte(1); i <= 3; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key"), Value: []byte{i}},
					},
				},
			},
			{
				Name: EVMStoreName,
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: storageKey, Value: []byte{i}},
					},
				},
			},
		})
		require.NoError(t, err)
		_, err = cs.Commit()
		require.NoError(t, err)
	}
	require.Equal(t, int64(3), cs.cosmosCommitter.Version())
	require.Equal(t, int64(3), cs.evmCommitter.Version())
	require.NoError(t, cs.Close())

	// Simulate crash: rollback FlatKV to version 2 independently, leaving
	// cosmos at version 3. This mirrors a crash after cosmos Commit but
	// before FlatKV Commit completes.
	flatkvPath := dir + "/data/flatkv"
	evmStore := flatkv.NewCommitStore(t.Context(), flatkvPath, cfg.FlatKVConfig)
	_, err = evmStore.LoadVersion(0, false)
	require.NoError(t, err)
	require.Equal(t, int64(3), evmStore.Version())
	err = evmStore.Rollback(2)
	require.NoError(t, err)
	require.Equal(t, int64(2), evmStore.Version())
	require.NoError(t, evmStore.Close())

	// Reopen the composite store — LoadVersion(0) should detect the
	// mismatch and reconcile both backends to version 2.
	cs2 := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs2.Initialize([]string{"test", EVMStoreName})
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs2.Close()

	require.Equal(t, int64(2), cs2.cosmosCommitter.Version(), "cosmos should be rolled back to EVM version")
	require.Equal(t, int64(2), cs2.evmCommitter.Version(), "EVM should remain at version 2")
	require.Equal(t, int64(2), cs2.Version())

	// Verify cosmos data is at version 2 (value = 0x02, not 0x03)
	testStore := cs2.GetChildStoreByName("test")
	require.NotNil(t, testStore)
	require.Equal(t, []byte{2}, testStore.Get([]byte("key")))
}

func TestReconcileVersionsCosmosAheadByMultiple(t *testing.T) {
	addr := [20]byte{0xCC}
	slot := [32]byte{0xDD}
	storageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage,
		flatkv.StorageKey(addr, slot))

	cfg := splitWriteConfig()

	dir := t.TempDir()
	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"bank", EVMStoreName})
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	for i := byte(1); i <= 5; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "bank",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("bal"), Value: []byte{i}},
					},
				},
			},
			{
				Name: EVMStoreName,
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: storageKey, Value: []byte{i}},
					},
				},
			},
		})
		require.NoError(t, err)
		_, err = cs.Commit()
		require.NoError(t, err)
	}
	require.NoError(t, cs.Close())

	// Rollback FlatKV to version 3 (simulating 2 lost commits)
	flatkvPath := dir + "/data/flatkv"
	evmStore := flatkv.NewCommitStore(t.Context(), flatkvPath, cfg.FlatKVConfig)
	_, err = evmStore.LoadVersion(0, false)
	require.NoError(t, err)
	err = evmStore.Rollback(3)
	require.NoError(t, err)
	require.NoError(t, evmStore.Close())

	cs2 := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs2.Initialize([]string{"bank", EVMStoreName})
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs2.Close()

	require.Equal(t, int64(3), cs2.cosmosCommitter.Version())
	require.Equal(t, int64(3), cs2.evmCommitter.Version())

	bankStore := cs2.GetChildStoreByName("bank")
	require.Equal(t, []byte{3}, bankStore.Get([]byte("bal")))
}
