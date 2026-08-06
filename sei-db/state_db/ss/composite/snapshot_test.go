package composite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/cosmos"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/stretchr/testify/require"
)

type noCheckpointStateStore struct {
	types.StateStore
}

func bankChangeset(key, value string) []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{{Key: []byte(key), Value: []byte(value)}},
			},
		},
	}
}

// evmStorageKey builds a key in the EVM storage family (0x03 prefix), which
// routes to the storage sub-DB when sub-DBs are separate.
func evmStorageKey() []byte {
	return append([]byte{0x03}, make([]byte, 20+32)...)
}

// setupSnapshotStore opens a store with snapshotting on at a small interval so
// tests can cross boundaries cheaply. It returns the store and its snapshots
// root.
func setupSnapshotStore(t *testing.T, interval int64, keepRecent int, separateEVMSubDBs bool) (*CompositeStateStore, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewCompositeStateStore(config.StateStoreConfig{
		Backend:            "pebbledb",
		AsyncWriteBuffer:   100,
		KeepRecent:         100000,
		EVMSplit:           true,
		SeparateEVMSubDBs:  separateEVMSubDBs,
		EVMDBDirectory:     filepath.Join(dir, "evm_ss"),
		SnapshotEnable:     true,
		SnapshotInterval:   interval,
		SnapshotKeepRecent: keepRecent,
	}, dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NotNil(t, store.snapshotMgr)
	return store, filepath.Join(dir, "data", "state_store", SnapshotsDirName)
}

// settle waits until every snapshot requested so far has been published and its
// pruning finished. Snapshot barriers sit in the backends' apply queues, so
// draining those queues is what guarantees the barriers ran.
func settle(t *testing.T, store *CompositeStateStore) {
	t.Helper()
	store.waitForPendingWrites()
	store.snapshotMgr.publishing.Wait()
}

func writeBlock(t *testing.T, store *CompositeStateStore, version int64) {
	t.Helper()
	require.NoError(t, store.ApplyChangesetAsync(version, []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{{Key: []byte("balance"), Value: []byte{byte(version)}}},
			},
		},
		{
			Name: evm.EVMStoreKey,
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{{Key: evmStorageKey(), Value: []byte{byte(version)}}},
			},
		},
	}))
}

// The snapshot manager keys off the mirrored cadence, so the ss-snapshot-enable
// switch has to reach it as a zero interval and leave no manager running.
func TestSnapshotManagerRespectsSnapshotEnable(t *testing.T) {
	for _, tc := range []struct {
		name        string
		enable      bool
		wantRunning bool
	}{
		{name: "enabled", enable: true, wantRunning: true},
		{name: "disabled", enable: false, wantRunning: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ssConfig := config.DefaultStateStoreConfig()
			ssConfig.SnapshotEnable = tc.enable
			config.AlignSSSnapshotWithSC(config.DefaultStateCommitConfig(), &ssConfig)

			store, err := NewCompositeStateStore(ssConfig, dir)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })

			if tc.wantRunning {
				require.NotNil(t, store.snapshotMgr, "snapshots are on by default")
				require.Positive(t, ssConfig.SnapshotInterval)
			} else {
				require.Nil(t, store.snapshotMgr)
				require.Zero(t, ssConfig.SnapshotInterval)
			}
		})
	}
}

func TestSnapshotManagerDeclinesUnsupportedBackend(t *testing.T) {
	store := &CompositeStateStore{
		cosmosStore: cosmos.NewCosmosStateStore(&noCheckpointStateStore{}),
		config: config.StateStoreConfig{
			Backend:          config.RocksDBBackend,
			SnapshotInterval: 10,
		},
	}

	store.startSnapshotManager(t.TempDir())
	require.Nil(t, store.snapshotMgr)
}

// Snapshot labels are the interval boundaries themselves, not whatever version
// the store happened to be at when some background pass noticed. That is the
// property the in-queue barrier buys, and it is what makes an SS snapshot line
// up with the SC snapshot at the same height.
func TestSnapshotTakenAtExactIntervalBoundaries(t *testing.T) {
	store, root := setupSnapshotStore(t, 5, 5, false)

	for v := int64(1); v <= 12; v++ {
		writeBlock(t, store, v)
	}
	settle(t, store)

	versions, err := ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{5, 10}, versions,
		"snapshots must land on interval boundaries and nowhere else")

	target, err := os.Readlink(filepath.Join(root, snapshotCurrentLink))
	require.NoError(t, err)
	require.Equal(t, SnapshotDirName(10), target)
}

func TestSnapshotTakenAtExactIntervalBoundaryWithoutEVMSplit(t *testing.T) {
	dir := t.TempDir()
	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = config.PebbleDBBackend
	ssConfig.AsyncWriteBuffer = 100
	ssConfig.KeepRecent = 100000
	ssConfig.EVMSplit = false
	ssConfig.SnapshotEnable = true
	ssConfig.SnapshotInterval = 5
	ssConfig.SnapshotKeepRecent = 1

	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NotNil(t, store.snapshotMgr)

	for version := int64(1); version <= 5; version++ {
		require.NoError(t, store.ApplyChangesetAsync(version, bankChangeset("balance", "value")))
	}
	settle(t, store)

	root := filepath.Join(dir, "data", "state_store", SnapshotsDirName)
	versions, err := ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{5}, versions)
}

// A snapshot must be a complete image of every version at or below its label,
// reopenable as a store in its own right.
func TestSnapshotReopensWithEveryVersionBelowLabel(t *testing.T) {
	store, root := setupSnapshotStore(t, 10, 5, false)

	for v := int64(1); v <= 10; v++ {
		writeBlock(t, store, v)
	}
	settle(t, store)

	const label = int64(10)
	snapDir := filepath.Join(root, SnapshotDirName(label))
	require.DirExists(t, snapDir)

	reopened, err := NewCompositeStateStore(config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		EVMSplit:         true,
		DBDirectory:      filepath.Join(snapDir, "cosmos", "pebbledb"),
		EVMDBDirectory:   filepath.Join(snapDir, "evm", "pebbledb"),
	}, t.TempDir())
	require.NoError(t, err)
	defer reopened.Close()

	require.Equal(t, label, reopened.GetLatestVersion(),
		"the label is the version the snapshot was requested at")
	for v := int64(1); v <= label; v++ {
		val, err := reopened.Get("bank", v, []byte("balance"))
		require.NoError(t, err)
		require.Equal(t, []byte{byte(v)}, val, "cosmos version %d missing from snapshot", v)
		val, err = reopened.Get(evm.EVMStoreKey, v, evmStorageKey())
		require.NoError(t, err)
		require.Equal(t, []byte{byte(v)}, val, "evm version %d missing from snapshot", v)
	}
}

// The reason the barrier has to be a message in every queue rather than a wait:
// a block that only touches storage keys is enqueued only on the storage sub-DB,
// so the idle sub-DBs never observe that version and no amount of waiting would
// tell them it passed. Every sub-DB must still be captured.
func TestSnapshotCapturesIdleEVMSubDBs(t *testing.T) {
	store, root := setupSnapshotStore(t, 5, 5, true)

	// Storage keys only: codehash, code and misc sub-DBs stay idle throughout.
	for v := int64(1); v <= 5; v++ {
		require.NoError(t, store.ApplyChangesetAsync(v, []*proto.NamedChangeSet{
			{
				Name: evm.EVMStoreKey,
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{{Key: evmStorageKey(), Value: []byte{byte(v)}}},
				},
			},
		}))
	}
	settle(t, store)

	evmRoot := filepath.Join(root, SnapshotDirName(5), "evm", "pebbledb")
	for _, storeType := range evm.AllEVMStoreTypes() {
		name := evm.StoreTypeName(storeType)
		subDir := filepath.Join(evmRoot, name)
		require.DirExists(t, subDir, "sub-DB %s missing from snapshot", name)
		// A checkpoint always carries a manifest. An empty directory would mean
		// the barrier never reached that sub-DB.
		manifests, err := filepath.Glob(filepath.Join(subDir, "MANIFEST-*"))
		require.NoError(t, err)
		require.NotEmpty(t, manifests, "sub-DB %s was not checkpointed", name)
	}

	// The storage sub-DB is the one that actually took writes, and it must be
	// readable at every version up to the label.
	storage, err := NewCompositeStateStore(config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		// EVM sub-DBs are opened with the plain byte comparer.
		UseDefaultComparer: true,
		DBDirectory:        filepath.Join(evmRoot, evm.StoreTypeName(evm.StoreStorage)),
	}, t.TempDir())
	require.NoError(t, err)
	defer storage.Close()

	for v := int64(1); v <= 5; v++ {
		val, err := storage.Get(evm.EVMStoreKey, v, evmStorageKey())
		require.NoError(t, err)
		require.Equal(t, []byte{byte(v)}, val, "evm storage version %d missing from snapshot", v)
	}
}

// An interval boundary that happens to be an empty block arrives through
// SetLatestVersion rather than the changeset path, and must still snapshot —
// otherwise a quiet chain skips whole intervals.
func TestSnapshotTakenOnEmptyBoundaryBlock(t *testing.T) {
	store, root := setupSnapshotStore(t, 5, 5, false)

	for v := int64(1); v <= 4; v++ {
		writeBlock(t, store, v)
	}
	// Block 5 is empty: marker only, nothing enqueued.
	require.NoError(t, store.SetLatestVersion(5))
	settle(t, store)

	versions, err := ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{5}, versions)

	// Every data version below the label is inside the snapshot, which is what
	// the label promises. The on-disk marker can trail it, because an applied
	// batch stamps the marker with its own version and block 5 wrote none.
	snapDir := filepath.Join(root, SnapshotDirName(5))
	reopened, err := NewCompositeStateStore(config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		DBDirectory:      filepath.Join(snapDir, "cosmos", "pebbledb"),
	}, t.TempDir())
	require.NoError(t, err)
	defer reopened.Close()

	val, err := reopened.Get("bank", 4, []byte("balance"))
	require.NoError(t, err)
	require.Equal(t, []byte{4}, val)
}

// TestSnapshotPrune verifies retention: with keepRecent=1, only the newest two
// snapshots survive and current tracks the newest.
func TestSnapshotPrune(t *testing.T) {
	store, root := setupSnapshotStore(t, 5, 1, false)

	for v := int64(1); v <= 15; v++ {
		writeBlock(t, store, v)
	}
	settle(t, store)

	versions, err := ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{10, 15}, versions)

	target, err := os.Readlink(filepath.Join(root, snapshotCurrentLink))
	require.NoError(t, err)
	require.Equal(t, SnapshotDirName(15), target)
}

// A rollback abandons the versions those snapshots image, so they must not
// survive it — an archive or a state-sync restore would otherwise serve the
// discarded branch, and `current` would point right at it.
func TestSnapshotsAboveRollbackTargetAreDiscarded(t *testing.T) {
	store, root := setupSnapshotStore(t, 5, 5, false)

	for v := int64(1); v <= 10; v++ {
		writeBlock(t, store, v)
	}
	settle(t, store)
	versions, err := ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{5, 10}, versions)

	require.NoError(t, store.Rollback(7))

	versions, err = ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{5}, versions, "the snapshot above the target must be gone")

	target, err := os.Readlink(filepath.Join(root, snapshotCurrentLink))
	require.NoError(t, err)
	require.Equal(t, SnapshotDirName(5), target, "current must fall back to the newest survivor")
}

// A crash mid-snapshot leaves a staging directory named after the boundary it
// was staging, which would otherwise sit there until that exact boundary came
// round again.
func TestStaleSnapshotTmpDirRemovedAtStartup(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "data", "state_store", SnapshotsDirName)
	stale := filepath.Join(root, snapshotTmpPrefix+SnapshotDirName(40))
	require.NoError(t, os.MkdirAll(filepath.Join(stale, "cosmos"), 0o750))

	ssConfig := config.DefaultStateStoreConfig()
	config.AlignSSSnapshotWithSC(config.DefaultStateCommitConfig(), &ssConfig)
	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoDirExists(t, stale)
}

func TestParseSnapshotVersion(t *testing.T) {
	v, ok := ParseSnapshotVersion(SnapshotDirName(219140000))
	require.True(t, ok)
	require.Equal(t, int64(219140000), v)

	for _, bad := range []string{"snapshot-", "snapshot-123", "current", "tmp-snapshot-00000000000000000010", "snapshot-0000000000000000001x"} {
		_, ok := ParseSnapshotVersion(bad)
		require.False(t, ok, "expected %q to be rejected", bad)
	}
}
