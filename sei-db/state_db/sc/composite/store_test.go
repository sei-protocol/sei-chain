package composite

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// failingEVMStore is a mock flatkv.Store whose LoadVersion always fails.
type failingEVMStore struct{}

var _ flatkv.Store = (*failingEVMStore)(nil)

func (f *failingEVMStore) LoadVersion(int64, bool) (flatkv.Store, error) {
	return nil, fmt.Errorf("flatkv unavailable")
}
func (f *failingEVMStore) ApplyChangeSets([]*proto.NamedChangeSet) error { return nil }
func (f *failingEVMStore) Commit() (int64, error)                        { return 0, nil }
func (f *failingEVMStore) Get(string, []byte) ([]byte, bool)             { return nil, false }
func (f *failingEVMStore) GetBlockHeightModified(string, []byte) (int64, bool, error) {
	return -1, false, nil
}
func (f *failingEVMStore) Has(string, []byte) bool                { return false }
func (f *failingEVMStore) RawGlobalIterator() flatkv.Iterator     { return nil }
func (f *failingEVMStore) RootHash() []byte                       { return nil }
func (f *failingEVMStore) Version() int64                         { return 0 }
func (f *failingEVMStore) WriteSnapshot(string) error             { return nil }
func (f *failingEVMStore) Rollback(int64) error                   { return nil }
func (f *failingEVMStore) Exporter(int64) (types.Exporter, error) { return nil, nil }
func (f *failingEVMStore) Importer(int64) (types.Importer, error) { return nil, nil }
func (f *failingEVMStore) GetPhaseTimer() *metrics.PhaseTimer     { return nil }
func (f *failingEVMStore) CommittedRootHash() []byte              { return nil }
func (f *failingEVMStore) Close() error                           { return nil }

func TestCompositeStoreBasicOperations(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()

	cs := NewCompositeCommitStore(t.Context(), dir, cfg)
	cs.Initialize([]string{"test", keys.EVMStoreKey})

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
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: []byte("key1"), Value: []byte("value1")},
				},
			},
		},
		{
			Name: keys.EVMStoreKey,
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
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

	evmStore := cs.GetChildStoreByName(keys.EVMStoreKey)
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
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
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
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
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
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
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
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
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
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
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
	cs.flatkvCommitter = &failingEVMStore{}

	readOnly, err := cs.LoadVersion(0, true)
	require.NoError(t, err, "readonly LoadVersion should succeed even when FlatKV fails")
	defer func() { _ = readOnly.Close() }()

	compositeRO, ok := readOnly.(*CompositeCommitStore)
	require.True(t, ok)
	require.Nil(t, compositeRO.flatkvCommitter, "flatkvCommitter should be nil when FlatKV failed")

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
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
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
		require.NotEqual(t, keys.FlatKVStoreKey, it.moduleName,
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

	require.NoError(t, imp.AddModule(keys.FlatKVStoreKey))
	imp.AddNode(&types.SnapshotNode{Key: []byte("k2"), Value: []byte("v2")})

	require.NoError(t, imp.AddModule("staking"))
	imp.AddNode(&types.SnapshotNode{Key: []byte("k3"), Value: []byte("v3")})

	// bank and staking → cosmos only
	require.Equal(t, []string{"bank", "staking"}, cosmosModules)
	require.Len(t, cosmosNodes, 2)
	require.Equal(t, []byte("k1"), cosmosNodes[0].Key)
	require.Equal(t, []byte("k3"), cosmosNodes[1].Key)

	// evm_flatkv → evm only
	require.Equal(t, []string{keys.FlatKVStoreKey}, evmModules)
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
