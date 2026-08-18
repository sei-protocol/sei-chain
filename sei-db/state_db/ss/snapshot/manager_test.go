package snapshot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/management"
	"github.com/stretchr/testify/require"
)

type controlledScheduler struct {
	pending chan func()
	fail    bool
}

func (*controlledScheduler) SupportsCheckpoint() bool {
	return true
}

func (s *controlledScheduler) ScheduleCheckpoint(destDir string, shouldRun func() bool, done func(error)) {
	s.pending <- func() {
		if !shouldRun() {
			done(management.ErrCheckpointCanceled)
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

func (*controlledScheduler) SetCheckpointVersion(string, int64) error {
	return nil
}

func TestManagerCommitPublishesCurrentAndPrunesByCount(t *testing.T) {
	root := t.TempDir()
	scheduler := &controlledScheduler{pending: make(chan func(), 1)}
	manager := openManager(t, root, scheduler, 1, false)

	stageAndCommit(t, manager, scheduler, 10)
	stageAndCommit(t, manager, scheduler, 20)
	stageAndCommit(t, manager, scheduler, 30)

	require.NoDirExists(t, filepath.Join(root, SnapshotDirName(10)))
	require.DirExists(t, filepath.Join(root, SnapshotDirName(20)))
	require.DirExists(t, filepath.Join(root, SnapshotDirName(30)))
	target, err := os.Readlink(filepath.Join(root, snapshotCurrentLink))
	require.NoError(t, err)
	require.Equal(t, SnapshotDirName(30), target)
}

func TestManagerPruneKeepsCurrentTarget(t *testing.T) {
	root := t.TempDir()
	for _, version := range []int64{5, 10, 15} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, SnapshotDirName(version)), 0o750))
	}
	require.NoError(t, os.Symlink(SnapshotDirName(5), filepath.Join(root, snapshotCurrentLink)))
	manager := openManager(t, root, &controlledScheduler{pending: make(chan func(), 1)}, 1, false)

	versions, err := manager.Versions()
	require.NoError(t, err)
	require.Equal(t, []int64{5, 10, 15}, versions)
}

func TestManagerPruneSnapshotsByHeight(t *testing.T) {
	root := t.TempDir()
	for _, version := range []int64{5, 10, 15} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, SnapshotDirName(version)), 0o750))
	}
	manager := openManager(t, root, &controlledScheduler{pending: make(chan func(), 1)}, 10, false)

	require.NoError(t, manager.PruneSnapshots(12))

	versions, err := manager.Versions()
	require.NoError(t, err)
	require.Equal(t, []int64{15}, versions)
}

func TestManagerExternalPruningStandsDownInternalRetention(t *testing.T) {
	root := t.TempDir()
	scheduler := &controlledScheduler{pending: make(chan func(), 1)}
	manager := openManager(t, root, scheduler, 0, true)

	stageAndCommit(t, manager, scheduler, 10)
	stageAndCommit(t, manager, scheduler, 20)

	versions, err := manager.Versions()
	require.NoError(t, err)
	require.Equal(t, []int64{10, 20}, versions,
		"keepRecent would have dropped 10 if internal retention still ran")
}

// PruneSnapshots is how an external collector prunes this store, and external pruning is the only mode
// it is called in, so the cut line must be honored there. The current snapshot survives it.
func TestManagerPruneSnapshotsActsUnderExternalPruning(t *testing.T) {
	root := t.TempDir()
	scheduler := &controlledScheduler{pending: make(chan func(), 1)}
	manager := openManager(t, root, scheduler, 0, true)

	stageAndCommit(t, manager, scheduler, 10)
	stageAndCommit(t, manager, scheduler, 20)

	require.NoError(t, manager.PruneSnapshots(20))

	versions, err := manager.Versions()
	require.NoError(t, err)
	require.Equal(t, []int64{20}, versions)
}

// Retention counts only this member's directories, so a height an unpaired newer snapshot has pushed out
// of the keep window can still be the newest one every member holds — the height a restore starts from.
func TestManagerRetentionKeepsTheSharedFloor(t *testing.T) {
	root := t.TempDir()
	for _, version := range []int64{10, 20, 30} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, SnapshotDirName(version)), 0o750))
	}
	floor := NewFloor(10)
	scheduler := &controlledScheduler{pending: make(chan func(), 1)}
	manager := openManagerWithFloor(t, root, scheduler, 0, false, floor)

	versions, err := manager.Versions()
	require.NoError(t, err)
	require.Equal(t, []int64{10, 30}, versions, "only the unpaired 20 is beyond the keep window")

	// The members agree again, so the height they used to share is free to go.
	floor.Set(30)
	require.NoError(t, manager.PruneSnapshots(30))

	versions, err = manager.Versions()
	require.NoError(t, err)
	require.Equal(t, []int64{30}, versions)
}

// A hardlink probe left by a crash is reclaimed rather than accumulating, in the source directory and in
// the snapshot root alike.
func TestOpenClearsLeftoverHardlinkProbes(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(source, linkProbeName), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, linkProbeName), nil, 0o600))

	_, err := Open(Config{
		Name:       "test",
		Root:       root,
		SourceDirs: []string{source},
		Backend:    config.PebbleDBBackend,
		Scheduler:  &controlledScheduler{pending: make(chan func(), 1)},
	})
	require.NoError(t, err)

	require.NoFileExists(t, filepath.Join(source, linkProbeName))
	require.NoFileExists(t, filepath.Join(root, linkProbeName))
}

func TestManagerAbortRemovesStagedSnapshot(t *testing.T) {
	root := t.TempDir()
	scheduler := &controlledScheduler{pending: make(chan func(), 1)}
	manager := openManager(t, root, scheduler, 1, false)

	staged := stage(t, manager, scheduler, 10)
	staged.Abort()

	require.NoDirExists(t, filepath.Join(root, snapshotTmpPrefix+SnapshotDirName(10)))
	require.NoDirExists(t, filepath.Join(root, SnapshotDirName(10)))
}

// A staging directory that already exists may hold a checkpoint still being written, so preparing the
// same version again must refuse rather than clear it: clearing it publishes whatever the interrupted
// checkpoint had reached under a label that claims to be exact.
func TestManagerPrepareRefusesExistingStagingDir(t *testing.T) {
	root := t.TempDir()
	scheduler := &controlledScheduler{pending: make(chan func(), 1)}
	manager := openManager(t, root, scheduler, 1, false)

	staged := stage(t, manager, scheduler, 10)
	require.DirExists(t, staged.tmpDir)

	_, err := manager.Prepare(10)
	require.Error(t, err)
	require.DirExists(t, staged.tmpDir, "the first attempt's checkpoint must survive the refusal")
}

func openManager(t *testing.T, root string, scheduler *controlledScheduler, keepRecent int, external bool) *Manager {
	t.Helper()
	return openManagerWithFloor(t, root, scheduler, keepRecent, external, nil)
}

func openManagerWithFloor(
	t *testing.T,
	root string,
	scheduler *controlledScheduler,
	keepRecent int,
	external bool,
	floor *Floor,
) *Manager {
	t.Helper()
	manager, err := Open(Config{
		Name:            "test",
		Root:            root,
		SourceDirs:      []string{t.TempDir()},
		Backend:         config.PebbleDBBackend,
		KeepRecent:      keepRecent,
		ExternalPruning: external,
		Scheduler:       scheduler,
		Floor:           floor,
	})
	require.NoError(t, err)
	return manager
}

func stageAndCommit(t *testing.T, manager *Manager, scheduler *controlledScheduler, version int64) {
	t.Helper()
	staged := stage(t, manager, scheduler, version)
	require.NoError(t, manager.Commit(staged))
}

func stage(t *testing.T, manager *Manager, scheduler *controlledScheduler, version int64) *Staged {
	t.Helper()
	staged, err := manager.Prepare(version)
	require.NoError(t, err)
	manager.Schedule(staged, func() bool { return true }, func(err error) {
		require.NoError(t, err)
	})
	(<-scheduler.pending)()
	return staged
}
