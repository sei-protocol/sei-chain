package composite

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/management"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/cosmos"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/stretchr/testify/require"
)

type noCheckpointStateStore struct {
	types.StateStore
}

type noBarrierStateStore struct {
	types.StateStore
}

func (*noBarrierStateStore) Checkpoint(string) error {
	return nil
}

func (*noBarrierStateStore) SetCheckpointVersion(string, int64) error {
	return nil
}

type controlledSnapshotScheduler struct {
	pending         chan func()
	entered         chan struct{}
	checkpointCalls int
}

func (*controlledSnapshotScheduler) SupportsCheckpoint() bool {
	return true
}

func (s *controlledSnapshotScheduler) ScheduleCheckpoint(
	destDir string,
	shouldRun func() bool,
	done func(error),
) {
	if s.entered != nil {
		close(s.entered)
	}
	s.pending <- func() {
		if !shouldRun() {
			done(management.ErrCheckpointCanceled)
			return
		}
		s.checkpointCalls++
		_ = os.MkdirAll(destDir, 0o750)
		done(nil)
	}
}

func (*controlledSnapshotScheduler) SetCheckpointVersion(string, int64) error {
	return nil
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

type pendingWaiter interface {
	WaitForPendingWrites()
}

// settle waits until every snapshot requested so far has been published and its
// pruning finished. Snapshot barriers sit in the backends' apply queues, so
// draining those queues is what guarantees the barriers ran.
func settle(t *testing.T, store *CompositeStateStore) {
	t.Helper()
	if w, ok := store.cosmosStore.(pendingWaiter); ok {
		w.WaitForPendingWrites()
	}
	if w, ok := store.evmStore.(pendingWaiter); ok {
		w.WaitForPendingWrites()
	}
	store.snapshotMgr.publishing.Wait()
}

// commitBlock is what rootmulti.flush does for a populated block: enqueue the
// changesets, then hand the version to the snapshot manager. ApplyChangesetAsync
// alone schedules nothing, so tests that expect a snapshot must come through
// here.
func commitBlock(t *testing.T, store *CompositeStateStore, version int64, changesets []*proto.NamedChangeSet) {
	t.Helper()
	require.NoError(t, store.ApplyChangesetAsync(version, changesets))
	store.ScheduleSnapshot(version)
}

func writeBlock(t *testing.T, store *CompositeStateStore, version int64) {
	t.Helper()
	commitBlock(t, store, version, []*proto.NamedChangeSet{
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
	})
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
				require.NotNil(t, store.snapshotMgr, "explicit opt-in starts snapshotting")
				require.Positive(t, ssConfig.SnapshotInterval)
			} else {
				require.Nil(t, store.snapshotMgr)
				require.Zero(t, ssConfig.SnapshotInterval)
			}
		})
	}
}

func TestCustomStateStoreDirectoryMovesSnapshotRootBesideDatabase(t *testing.T) {
	home := t.TempDir()
	customDB := filepath.Join(t.TempDir(), "cosmos-state")
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = config.PebbleDBBackend
	cfg.DBDirectory = customDB
	cfg.SnapshotEnable = true
	cfg.SnapshotInterval = 5
	cfg.SnapshotKeepRecent = 1

	store, err := NewCompositeStateStore(cfg, home)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.Equal(t, customDB+"-"+SnapshotsDirName, store.snapshotMgr.root)
}

func TestSnapshotHardlinkPreflightCleansProbeFiles(t *testing.T) {
	source := t.TempDir()
	root := t.TempDir()
	require.NoError(t, verifySnapshotHardlinks(root, []string{source}))

	sourceEntries, err := os.ReadDir(source)
	require.NoError(t, err)
	require.Empty(t, sourceEntries)
	rootEntries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, rootEntries)
}

func TestSnapshotHardlinkPreflightRejectsCrossFilesystem(t *testing.T) {
	root, err := os.MkdirTemp("/dev/shm", "ss-snapshot-test-*")
	if err != nil {
		t.Skipf("no separate /dev/shm filesystem: %v", err)
	}
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })

	err = verifySnapshotHardlinks(root, []string{t.TempDir()})
	if err == nil {
		t.Skip("temporary directory and /dev/shm use the same filesystem")
	}
	require.ErrorContains(t, err, "cannot hardlink snapshots")
}

func TestSnapshotManagerRejectsUnsupportedBackend(t *testing.T) {
	store := &CompositeStateStore{
		cosmosStore: cosmos.NewCosmosStateStore(&noCheckpointStateStore{}),
		config: config.StateStoreConfig{
			Backend:          config.RocksDBBackend,
			SnapshotInterval: 10,
		},
	}

	err := store.startSnapshotManager(t.TempDir(), nil)
	require.ErrorContains(t, err, "does not support checkpoints")
	require.Nil(t, store.snapshotMgr)
}

func TestSnapshotManagerRejectsBackendWithoutBarrier(t *testing.T) {
	store := &CompositeStateStore{
		cosmosStore: cosmos.NewCosmosStateStore(&noBarrierStateStore{}),
		config: config.StateStoreConfig{
			Backend:          config.PebbleDBBackend,
			SnapshotInterval: 10,
		},
	}

	err := store.startSnapshotManager(t.TempDir(), nil)
	require.ErrorContains(t, err, "does not support checkpoints")
	require.Nil(t, store.snapshotMgr)
}

func TestSnapshotStopCancelsQueuedCheckpoint(t *testing.T) {
	scheduler := &controlledSnapshotScheduler{pending: make(chan func(), 1)}
	manager := &snapshotManager{
		root:            t.TempDir(),
		backend:         config.PebbleDBBackend,
		interval:        5,
		keepRecent:      1,
		cosmosScheduler: scheduler,
	}

	manager.maybeSnapshot(5)
	manager.stop()
	(<-scheduler.pending)()

	require.Zero(t, scheduler.checkpointCalls)
	versions, err := ListSnapshotVersions(manager.root)
	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestSnapshotManagerAllowsOnlyOneInFlightSnapshot(t *testing.T) {
	scheduler := &controlledSnapshotScheduler{pending: make(chan func(), 2)}
	manager := &snapshotManager{
		root:            t.TempDir(),
		backend:         config.PebbleDBBackend,
		interval:        5,
		keepRecent:      1,
		cosmosScheduler: scheduler,
	}

	manager.maybeSnapshot(5)
	manager.maybeSnapshot(10)
	require.Len(t, scheduler.pending, 1, "a second boundary must not enqueue while one snapshot is active")

	(<-scheduler.pending)()
	manager.publishing.Wait()
	require.False(t, manager.inFlight)
	require.Equal(t, int64(5), manager.lastRequested)
}

func TestSnapshotManagerAppliesMinimumTimeInterval(t *testing.T) {
	scheduler := &controlledSnapshotScheduler{pending: make(chan func(), 2)}
	manager := &snapshotManager{
		root:            t.TempDir(),
		backend:         config.PebbleDBBackend,
		interval:        5,
		minTime:         time.Hour,
		keepRecent:      1,
		cosmosScheduler: scheduler,
	}

	manager.maybeSnapshot(5)
	(<-scheduler.pending)()
	manager.publishing.Wait()

	manager.maybeSnapshot(10)
	require.Empty(t, scheduler.pending, "a rapid boundary must be skipped")

	manager.mu.Lock()
	manager.lastRequestAt = time.Now().Add(-2 * time.Hour)
	manager.mu.Unlock()
	manager.maybeSnapshot(10)
	require.Len(t, scheduler.pending, 1)
	(<-scheduler.pending)()
	manager.publishing.Wait()
}

func TestSnapshotStopWaitsForBarrierScheduling(t *testing.T) {
	scheduler := &controlledSnapshotScheduler{
		pending: make(chan func()),
		entered: make(chan struct{}),
	}
	manager := &snapshotManager{
		root:            t.TempDir(),
		backend:         config.PebbleDBBackend,
		interval:        5,
		keepRecent:      1,
		cosmosScheduler: scheduler,
	}

	requestDone := make(chan struct{})
	go func() {
		manager.maybeSnapshot(5)
		close(requestDone)
	}()
	<-scheduler.entered

	stopDone := make(chan struct{})
	go func() {
		manager.stop()
		close(stopDone)
	}()
	require.Never(t, func() bool {
		select {
		case <-stopDone:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, 5*time.Millisecond)

	callback := <-scheduler.pending
	<-requestDone
	<-stopDone
	callback()
	require.False(t, manager.inFlight)
}

// Snapshot labels are the interval boundaries themselves, not whatever version
// the store happened to be at when some background pass noticed. That is the
// property the in-queue barrier buys. It keeps each accepted SS snapshot's
// contents aligned with its own label even when SC independently skips that
// boundary.
func TestSnapshotTakenAtExactIntervalBoundaries(t *testing.T) {
	store, root := setupSnapshotStore(t, 5, 5, false)

	for v := int64(1); v <= 12; v++ {
		writeBlock(t, store, v)
		if v%5 == 0 {
			settle(t, store)
		}
	}
	settle(t, store)

	versions, err := ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{5, 10}, versions,
		"snapshots must land on interval boundaries and nowhere else")

	target, err := os.Readlink(filepath.Join(root, snapshotCurrentLink))
	require.NoError(t, err)
	require.Equal(t, SnapshotDirName(10), target)

	snapDir := filepath.Join(root, SnapshotDirName(10))
	apparentBytes, err := readSnapshotSize(snapDir)
	require.NoError(t, err)
	require.Positive(t, apparentBytes)
	reopened, err := NewCompositeStateStore(config.StateStoreConfig{
		Backend:          config.PebbleDBBackend,
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		EVMSplit:         true,
		DBDirectory:      filepath.Join(snapDir, "cosmos", config.PebbleDBBackend),
		EVMDBDirectory:   filepath.Join(snapDir, "evm", config.PebbleDBBackend),
	}, t.TempDir())
	require.NoError(t, err)
	defer reopened.Close()

	require.Equal(t, int64(10), reopened.GetLatestVersion())
	cosmosValue, err := reopened.Get("bank", 12, []byte("balance"))
	require.NoError(t, err)
	require.Equal(t, []byte{10}, cosmosValue, "snapshot 10 must exclude Cosmos writes 11 and 12")
	evmValue, err := reopened.Get(evm.EVMStoreKey, 12, evmStorageKey())
	require.NoError(t, err)
	require.Equal(t, []byte{10}, evmValue, "snapshot 10 must exclude EVM writes 11 and 12")
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
		commitBlock(t, store, version, bankChangeset("balance", "value"))
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

// The property the barrier exists for: the label stays exact while the write
// path keeps going. Nothing is drained between block 10 and blocks 11 and 12, so
// the checkpoint runs with later versions already queued behind the barrier — the
// case a post-hoc "snapshot what has been applied" scheme would get wrong.
func TestSnapshotExcludesVersionsWrittenAfterTheBoundary(t *testing.T) {
	store, root := setupSnapshotStore(t, 10, 5, false)

	const label = int64(10)
	for v := int64(1); v <= label; v++ {
		writeBlock(t, store, v)
	}
	for v := label + 1; v <= label+2; v++ {
		writeBlock(t, store, v)
	}
	settle(t, store)

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

	require.Equal(t, label, reopened.GetLatestVersion())
	// Reading above the label returns the label's value rather than 11 or 12,
	// which is what "excluded" means for an MVCC store: the later writes are not
	// in this image at any version.
	for _, above := range []int64{label + 1, label + 2} {
		val, err := reopened.Get("bank", above, []byte("balance"))
		require.NoError(t, err)
		require.Equal(t, []byte{byte(label)}, val,
			"cosmos read at %d saw a write from after the boundary", above)
		val, err = reopened.Get(evm.EVMStoreKey, above, evmStorageKey())
		require.NoError(t, err)
		require.Equal(t, []byte{byte(label)}, val,
			"evm read at %d saw a write from after the boundary", above)
	}

	// The live store keeps them, so the snapshot dropped them rather than the
	// writes never landing.
	val, err := store.Get("bank", label+2, []byte("balance"))
	require.NoError(t, err)
	require.Equal(t, []byte{byte(label + 2)}, val)
}

// A snapshot inherits each database's earliest marker. The composite is allowed
// to reopen with different member floors because it reports the highest one.
func TestSnapshotInheritsPerStoreEarliestMarkers(t *testing.T) {
	store, root := setupSnapshotStore(t, 10, 5, false)

	for v := int64(1); v <= 9; v++ {
		writeBlock(t, store, v)
	}
	settle(t, store)
	require.NoError(t, store.cosmosStore.SetEarliestVersion(2, false))
	require.NoError(t, store.evmStore.SetEarliestVersion(5, false))
	require.Equal(t, int64(2), store.cosmosStore.GetEarliestVersion())
	require.Equal(t, int64(5), store.evmStore.GetEarliestVersion())

	writeBlock(t, store, 10)
	settle(t, store)

	snapDir := filepath.Join(root, SnapshotDirName(10))
	require.DirExists(t, snapDir)

	reopened, err := NewCompositeStateStore(config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		EVMSplit:         true,
		DBDirectory:      filepath.Join(snapDir, "cosmos", "pebbledb"),
		EVMDBDirectory:   filepath.Join(snapDir, "evm", "pebbledb"),
	}, t.TempDir())
	require.NoError(t, err, "a snapshot with different member floors must reopen")
	defer reopened.Close()

	require.Equal(t, int64(2), reopened.cosmosStore.GetEarliestVersion())
	require.Equal(t, int64(5), reopened.GetEarliestVersion())
	require.Equal(t, int64(5), reopened.evmStore.GetEarliestVersion())
}

func TestSnapshotInheritsEarliestMarkerAfterPrune(t *testing.T) {
	store, root := setupSnapshotStore(t, 10, 5, false)

	for v := int64(1); v <= 9; v++ {
		writeBlock(t, store, v)
	}
	settle(t, store)
	require.NoError(t, store.Prune(4))

	writeBlock(t, store, 10)
	settle(t, store)

	snapDir := filepath.Join(root, SnapshotDirName(10))
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

	require.Equal(t, int64(5), reopened.GetEarliestVersion())
	require.Equal(t, int64(5), reopened.cosmosStore.GetEarliestVersion())
	require.Equal(t, int64(5), reopened.evmStore.GetEarliestVersion())
}

// With separate sub-DBs the latest label has to reach every one of them, not
// just the sub-DBs that took writes, or the snapshot is not self-describing at
// its exact boundary.
func TestSnapshotSetsLatestVersionEveryEVMSubDB(t *testing.T) {
	store, root := setupSnapshotStore(t, 10, 5, true)

	for v := int64(1); v <= 9; v++ {
		writeBlock(t, store, v)
	}

	writeBlock(t, store, 10)
	settle(t, store)

	snapDir := filepath.Join(root, SnapshotDirName(10))
	reopened, err := NewCompositeStateStore(config.StateStoreConfig{
		Backend:           "pebbledb",
		AsyncWriteBuffer:  0,
		KeepRecent:        100000,
		EVMSplit:          true,
		SeparateEVMSubDBs: true,
		DBDirectory:       filepath.Join(snapDir, "cosmos", "pebbledb"),
		EVMDBDirectory:    filepath.Join(snapDir, "evm", "pebbledb"),
	}, t.TempDir())
	require.NoError(t, err)

	require.Equal(t, int64(10), reopened.evmStore.GetLatestVersion())
	require.NoError(t, reopened.Close())

	evmRoot := filepath.Join(snapDir, "evm", "pebbledb")
	for _, storeType := range evm.AllEVMStoreTypes() {
		subDir := filepath.Join(evmRoot, evm.StoreTypeName(storeType))
		subDB, err := NewCompositeStateStore(config.StateStoreConfig{
			Backend:            "pebbledb",
			AsyncWriteBuffer:   0,
			KeepRecent:         100000,
			UseDefaultComparer: true,
			DBDirectory:        subDir,
		}, t.TempDir())
		require.NoError(t, err)
		require.Equal(t, int64(10), subDB.GetLatestVersion(), "sub-DB %s latest marker", evm.StoreTypeName(storeType))
		require.NoError(t, subDB.Close())
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
		commitBlock(t, store, v, []*proto.NamedChangeSet{
			{
				Name: evm.EVMStoreKey,
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{{Key: evmStorageKey(), Value: []byte{byte(v)}}},
				},
			},
		})
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
	store.ScheduleSnapshot(5)
	settle(t, store)

	versions, err := ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{5}, versions)

	// Every data version below the label is inside the snapshot, and the
	// checkpoint marker advances to the empty block's version.
	snapDir := filepath.Join(root, SnapshotDirName(5))
	reopened, err := NewCompositeStateStore(config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		DBDirectory:      filepath.Join(snapDir, "cosmos", "pebbledb"),
	}, t.TempDir())
	require.NoError(t, err)
	defer reopened.Close()

	require.Equal(t, int64(5), reopened.GetLatestVersion())
	val, err := reopened.Get("bank", 4, []byte("balance"))
	require.NoError(t, err)
	require.Equal(t, []byte{4}, val)
}

func TestSetLatestVersionDoesNotSnapshotDuringImport(t *testing.T) {
	store, root := setupSnapshotStore(t, 5, 5, false)
	nodes := make(chan types.SnapshotNode)
	importDone := make(chan error, 1)
	go func() {
		importDone <- store.Import(5, nodes)
	}()
	closed := false
	t.Cleanup(func() {
		if !closed {
			close(nodes)
			<-importDone
		}
	})

	nodes <- types.SnapshotNode{StoreKey: "bank", Key: []byte("balance"), Value: []byte{5}}
	require.NoError(t, store.SetLatestVersion(5))
	settle(t, store)

	versions, err := ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Empty(t, versions, "direct restore metadata writes must not trigger a snapshot")

	close(nodes)
	closed = true
	require.NoError(t, <-importDone)
}

// TestSnapshotPrune verifies retention: with keepRecent=1, only the newest two
// snapshots survive and current tracks the newest.
func TestSnapshotPrune(t *testing.T) {
	store, root := setupSnapshotStore(t, 5, 1, false)

	for v := int64(1); v <= 15; v++ {
		writeBlock(t, store, v)
		if v%5 == 0 {
			settle(t, store)
		}
	}
	settle(t, store)

	versions, err := ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{10, 15}, versions)

	target, err := os.Readlink(filepath.Join(root, snapshotCurrentLink))
	require.NoError(t, err)
	require.Equal(t, SnapshotDirName(15), target)
}

func TestSnapshotManagerResumesFromNewestSnapshot(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = config.PebbleDBBackend
	cfg.AsyncWriteBuffer = 100
	cfg.KeepRecent = 100000
	cfg.SnapshotEnable = true
	cfg.SnapshotInterval = 5
	cfg.SnapshotKeepRecent = 1
	cfg.SnapshotMinTimeInterval = time.Hour

	store, err := NewCompositeStateStore(cfg, dir)
	require.NoError(t, err)
	for version := int64(1); version <= 5; version++ {
		commitBlock(t, store, version, bankChangeset("balance", "value"))
	}
	settle(t, store)

	root := filepath.Join(dir, "data", "state_store", SnapshotsDirName)
	snapshotDir := filepath.Join(root, SnapshotDirName(5))
	before, err := os.Stat(snapshotDir)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	require.NoError(t, os.Remove(filepath.Join(root, snapshotCurrentLink)))

	reopened, err := NewCompositeStateStore(cfg, dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	require.Equal(t, int64(5), reopened.snapshotMgr.lastRequested)
	require.WithinDuration(t, before.ModTime(), reopened.snapshotMgr.lastRequestAt, time.Second)
	target, err := os.Readlink(filepath.Join(root, snapshotCurrentLink))
	require.NoError(t, err)
	require.Equal(t, SnapshotDirName(5), target)

	reopened.ScheduleSnapshot(5)
	settle(t, reopened)
	after, err := os.Stat(snapshotDir)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after), "restart must not replace an existing boundary snapshot")

	for version := int64(6); version <= 10; version++ {
		commitBlock(t, reopened, version, bankChangeset("balance", "value"))
	}
	settle(t, reopened)
	versions, err := ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{5}, versions, "restart must preserve the minimum-time gate")
}

func TestOutOfOrderPublishDoesNotMoveCurrentBackward(t *testing.T) {
	root := t.TempDir()
	manager := &snapshotManager{root: root, keepRecent: 5}

	publish := func(version int64) {
		tmpDir := filepath.Join(root, snapshotTmpPrefix+SnapshotDirName(version))
		require.NoError(t, os.MkdirAll(tmpDir, 0o750))
		manager.publish(version, tmpDir, filepath.Join(root, SnapshotDirName(version)), time.Now())
	}
	publish(10)
	publish(5)

	target, err := os.Readlink(filepath.Join(root, snapshotCurrentLink))
	require.NoError(t, err)
	require.Equal(t, SnapshotDirName(10), target)
}

func TestSnapshotManagerPrunesExistingSnapshotsAtStartup(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "data", "state_store", SnapshotsDirName)
	for _, version := range []int64{5, 10, 15} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, SnapshotDirName(version)), 0o750))
	}

	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = config.PebbleDBBackend
	cfg.SnapshotEnable = true
	cfg.SnapshotInterval = 5
	cfg.SnapshotKeepRecent = 1
	store, err := NewCompositeStateStore(cfg, dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	versions, err := ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{10, 15}, versions)
}

func TestFailedPublishStillEnforcesRetention(t *testing.T) {
	root := t.TempDir()
	for _, version := range []int64{5, 10, 15} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, SnapshotDirName(version)), 0o750))
	}
	manager := &snapshotManager{root: root, keepRecent: 1}

	published := manager.publish(
		20,
		filepath.Join(root, "missing-staging-dir"),
		filepath.Join(root, SnapshotDirName(20)),
		time.Now(),
	)
	require.False(t, published)

	versions, err := ListSnapshotVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{10, 15}, versions)
}

func TestRetentionMetricsCacheSnapshotSizes(t *testing.T) {
	root := t.TempDir()
	snapshotDir := filepath.Join(root, SnapshotDirName(5))
	require.NoError(t, os.MkdirAll(snapshotDir, 0o750))
	dataFile := filepath.Join(snapshotDir, "data.sst")
	require.NoError(t, os.WriteFile(dataFile, []byte("one"), 0o600))
	require.NoError(t, writeSnapshotSize(snapshotDir, 3))

	manager := &snapshotManager{root: root}
	manager.recordRetentionMetrics()
	require.Equal(t, int64(3), manager.snapshotSizes[5])

	// Published snapshots are immutable, so later metric records reuse the
	// cached total rather than walking every retained hardlink tree again.
	require.NoError(t, os.WriteFile(dataFile, []byte("a longer value"), 0o600))
	manager.recordRetentionMetrics()
	require.Equal(t, int64(3), manager.snapshotSizes[5])

	require.NoError(t, os.RemoveAll(snapshotDir))
	manager.recordRetentionMetrics()
	require.NotContains(t, manager.snapshotSizes, int64(5))
}

func TestSnapshotRequestReturnsUnexpectedStatError(t *testing.T) {
	root := t.TempDir()
	name := SnapshotDirName(5)
	require.NoError(t, os.Symlink(name, filepath.Join(root, name)))

	manager := &snapshotManager{root: root, backend: config.PebbleDBBackend}
	err := manager.requestSnapshot(5, time.Now())
	require.ErrorContains(t, err, "inspect snapshot dir")
}

// A crash mid-snapshot leaves a staging directory named after the boundary it
// was staging, which would otherwise sit there until that exact boundary came
// round again.
func TestStaleSnapshotTmpDirRemovedAtStartup(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "data", "state_store", SnapshotsDirName)
	stale := filepath.Join(root, snapshotTmpPrefix+SnapshotDirName(40))
	require.NoError(t, os.MkdirAll(filepath.Join(stale, "cosmos"), 0o750))
	tmpLink := filepath.Join(root, snapshotCurrentTmpLink)
	require.NoError(t, os.Symlink(filepath.Base(stale), tmpLink))

	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.SnapshotEnable = true
	config.AlignSSSnapshotWithSC(config.DefaultStateCommitConfig(), &ssConfig)
	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoDirExists(t, stale)
	_, err = os.Lstat(tmpLink)
	require.ErrorIs(t, err, os.ErrNotExist)
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
