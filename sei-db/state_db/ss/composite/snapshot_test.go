package composite

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	sssnapshot "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/snapshot"
	"github.com/stretchr/testify/require"
)

type controlledSnapshotScheduler struct {
	pending chan func()
	fail    bool
}

func (*controlledSnapshotScheduler) SupportsCheckpoint() bool {
	return true
}

func (s *controlledSnapshotScheduler) ScheduleCheckpoint(
	destDir string,
	shouldRun func() bool,
	done func(error),
) {
	s.pending <- func() {
		if !shouldRun() {
			done(controller.ErrCheckpointCanceled)
			return
		}
		if s.fail {
			done(errors.New("checkpoint failed"))
			return
		}
		_ = os.MkdirAll(destDir, 0o750)
		done(nil)
	}
}

func (*controlledSnapshotScheduler) SetCheckpointVersion(string, int64) error {
	return nil
}

type pendingWaiter interface {
	WaitForPendingWrites()
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

func setupSnapshotStore(t *testing.T, interval int64, keepRecent int, separateEVMSubDBs bool) (*CompositeStateStore, string, string) {
	t.Helper()
	dir := t.TempDir()
	evmDir := filepath.Join(dir, "evm_ss")
	store, err := NewCompositeStateStore(config.StateStoreConfig{
		Backend:            "pebbledb",
		AsyncWriteBuffer:   100,
		KeepRecent:         100000,
		EVMSplit:           true,
		SeparateEVMSubDBs:  separateEVMSubDBs,
		EVMDBDirectory:     evmDir,
		SnapshotEnable:     true,
		SnapshotInterval:   interval,
		SnapshotKeepRecent: keepRecent,
	}, dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NotNil(t, store.snapshotMgr)
	return store,
		filepath.Join(dir, "data", "state_store", SnapshotsDirName),
		evmDir + "-" + SnapshotsDirName
}

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

func TestExternalPruningStandsDownWithoutSnapshots(t *testing.T) {
	cfg := config.DefaultStateStoreConfig()
	cfg.SnapshotEnable = false
	cfg.ExternalPruning = true

	store, err := NewCompositeStateStore(cfg, t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.Nil(t, store.snapshotMgr)
	require.Nil(t, store.pruningManager)
	require.True(t, store.cosmosStore.(interface{ ExternalPruning() bool }).ExternalPruning())
}

func TestPerStoreSnapshotRoots(t *testing.T) {
	store, cosmosRoot, evmRoot := setupSnapshotStore(t, 5, 1, true)

	require.Equal(t, cosmosRoot, store.cosmosStore.(cosmosStoreWithSnapshots).Snapshots().Root())
	require.Equal(t, evmRoot, store.evmStore.(*evm.EVMStateStore).Snapshots().Root())
}

func TestCustomStateStoreDirectoryMovesCosmosSnapshotRootBesideDatabase(t *testing.T) {
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

	require.Equal(t, customDB+"-"+SnapshotsDirName, store.cosmosStore.(cosmosStoreWithSnapshots).Snapshots().Root())
}

func TestCompositeCoordinatorPublishesEveryMember(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	schedulerA := &controlledSnapshotScheduler{pending: make(chan func(), 1)}
	schedulerB := &controlledSnapshotScheduler{pending: make(chan func(), 1)}
	managerA := openTestManager(t, "cosmos", rootA, schedulerA)
	managerB := openTestManager(t, "evm", rootB, schedulerB)
	coord := newTestSnapshotCoordinator(10, 0, []snapshotMember{
		{name: "cosmos", manager: managerA},
		{name: "evm", manager: managerB},
	})

	coord.maybeSnapshot(10)
	(<-schedulerA.pending)()
	(<-schedulerB.pending)()
	coord.publishing.Wait()

	require.DirExists(t, filepath.Join(rootA, SnapshotDirName(10)))
	require.DirExists(t, filepath.Join(rootB, SnapshotDirName(10)))
	require.Equal(t, int64(10), newestCommonSnapshot(coord.members))
}

func TestCompositeCoordinatorAbortsEveryMemberOnStageFailure(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	schedulerA := &controlledSnapshotScheduler{pending: make(chan func(), 1)}
	schedulerB := &controlledSnapshotScheduler{pending: make(chan func(), 1), fail: true}
	managerA := openTestManager(t, "cosmos", rootA, schedulerA)
	managerB := openTestManager(t, "evm", rootB, schedulerB)
	coord := newTestSnapshotCoordinator(10, 0, []snapshotMember{
		{name: "cosmos", manager: managerA},
		{name: "evm", manager: managerB},
	})

	coord.maybeSnapshot(10)
	(<-schedulerA.pending)()
	(<-schedulerB.pending)()
	coord.publishing.Wait()

	require.NoDirExists(t, filepath.Join(rootA, SnapshotDirName(10)))
	require.NoDirExists(t, filepath.Join(rootB, SnapshotDirName(10)))
}

// A request that fails must queue no barrier at all. The commit path can call maybeSnapshot twice for
// one block, so a failed request releases its version for another attempt; a barrier left behind by the
// first attempt would then be writing into the staging directory the retry reserves.
func TestCompositeCoordinatorQueuesNothingWhenAMemberCannotPrepare(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	schedulerA := &controlledSnapshotScheduler{pending: make(chan func(), 1)}
	schedulerB := &controlledSnapshotScheduler{pending: make(chan func(), 1)}
	managerA := openTestManager(t, "cosmos", rootA, schedulerA)
	managerB := openTestManager(t, "evm", rootB, schedulerB)
	coord := newTestSnapshotCoordinator(10, 0, []snapshotMember{
		{name: "cosmos", manager: managerA},
		{name: "evm", manager: managerB},
	})
	// Occupy the name the evm member would publish version 10 under, so its prepare fails after the
	// cosmos member has already been prepared.
	require.NoError(t, os.WriteFile(filepath.Join(rootB, SnapshotDirName(10)), nil, 0o600))

	coord.maybeSnapshot(10)

	require.Empty(t, schedulerA.pending, "a member must not be scheduled once another cannot prepare")
	require.Empty(t, schedulerB.pending)
	require.Equal(t, int64(0), coord.lastRequested, "the version stays available for another attempt")
	require.False(t, coord.inFlight)
}

func TestUnpairedSnapshotHeightsSurviveStartupAndCommonHeightIsLower(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(rootA, SnapshotDirName(10)), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(rootA, SnapshotDirName(20)), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(rootB, SnapshotDirName(10)), 0o750))

	// Retention that keeps one snapshot would drop 10 from the cosmos root, where the unpaired 20 holds
	// the only keep slot, leaving the members with no height in common to restore from.
	floor := sssnapshot.NewFloor(sssnapshot.NewestCommonVersion([]string{rootA, rootB}))
	schedulerA := &controlledSnapshotScheduler{pending: make(chan func(), 1)}
	schedulerB := &controlledSnapshotScheduler{pending: make(chan func(), 1)}
	managerA := openTestManagerWithRetention(t, "cosmos", rootA, schedulerA, 0, floor)
	managerB := openTestManagerWithRetention(t, "evm", rootB, schedulerB, 0, floor)
	coord := newSnapshotCoordinator(10, 0, []snapshotMember{
		{name: "cosmos", manager: managerA},
		{name: "evm", manager: managerB},
	}, floor)

	require.DirExists(t, filepath.Join(rootA, SnapshotDirName(10)), "the shared height must survive retention")
	require.DirExists(t, filepath.Join(rootA, SnapshotDirName(20)))
	require.Equal(t, int64(20), coord.lastRequested)
	require.Equal(t, int64(10), newestCommonSnapshot(coord.members))
	require.Equal(t, int64(10), floor.Height())
}

func TestSnapshotCoversEmptyBlock(t *testing.T) {
	store, cosmosRoot, evmRoot := setupSnapshotStore(t, 5, 1, false)

	for i := int64(1); i <= 4; i++ {
		writeBlock(t, store, i)
	}
	commitBlock(t, store, 5, nil)
	settle(t, store)

	require.DirExists(t, filepath.Join(cosmosRoot, SnapshotDirName(5)))
	require.DirExists(t, filepath.Join(evmRoot, SnapshotDirName(5)))
}

func TestSnapshotMinTimeIntervalSkipsBoundary(t *testing.T) {
	rootA := t.TempDir()
	scheduler := &controlledSnapshotScheduler{pending: make(chan func(), 1)}
	manager := openTestManager(t, "cosmos", rootA, scheduler)
	coord := newTestSnapshotCoordinator(10, time.Hour, []snapshotMember{{name: "cosmos", manager: manager}})

	coord.maybeSnapshot(10)
	(<-scheduler.pending)()
	coord.publishing.Wait()

	coord.maybeSnapshot(20)
	require.Empty(t, scheduler.pending)
	require.NoDirExists(t, filepath.Join(rootA, SnapshotDirName(20)))
}

func openTestManager(t *testing.T, name, root string, scheduler *controlledSnapshotScheduler) *sssnapshot.Manager {
	t.Helper()
	return openTestManagerWithRetention(t, name, root, scheduler, 1, nil)
}

func openTestManagerWithRetention(
	t *testing.T,
	name, root string,
	scheduler *controlledSnapshotScheduler,
	keepRecent int,
	floor *sssnapshot.Floor,
) *sssnapshot.Manager {
	t.Helper()
	source := t.TempDir()
	manager, err := sssnapshot.Open(sssnapshot.Config{
		Name:       name,
		Root:       root,
		SourceDirs: []string{source},
		Backend:    config.PebbleDBBackend,
		KeepRecent: keepRecent,
		Scheduler:  scheduler,
		Floor:      floor,
	})
	require.NoError(t, err)
	return manager
}

func newTestSnapshotCoordinator(
	interval int64,
	minTime time.Duration,
	members []snapshotMember,
) *snapshotCoordinator {
	return newSnapshotCoordinator(interval, minTime, members, sssnapshot.NewFloor(0))
}

type cosmosStoreWithSnapshots interface {
	Snapshots() *sssnapshot.Manager
}
