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
	require.NoError(t, manager.PruneSnapshots(20))

	versions, err := manager.Versions()
	require.NoError(t, err)
	require.Equal(t, []int64{10, 20}, versions)
}

func TestManagerAbortRemovesStagedSnapshot(t *testing.T) {
	root := t.TempDir()
	scheduler := &controlledScheduler{pending: make(chan func(), 1)}
	manager := openManager(t, root, scheduler, 1, false)
	var staged *Staged

	require.NoError(t, manager.Stage(10, func() bool { return true }, func(s *Staged, err error) {
		require.NoError(t, err)
		staged = s
	}))
	(<-scheduler.pending)()
	staged.Abort()

	require.NoDirExists(t, filepath.Join(root, snapshotTmpPrefix+SnapshotDirName(10)))
	require.NoDirExists(t, filepath.Join(root, SnapshotDirName(10)))
}

func openManager(t *testing.T, root string, scheduler *controlledScheduler, keepRecent int, external bool) *Manager {
	t.Helper()
	manager, err := Open(Config{
		Name:            "test",
		Root:            root,
		SourceDirs:      []string{t.TempDir()},
		Backend:         config.PebbleDBBackend,
		KeepRecent:      keepRecent,
		ExternalPruning: external,
		Scheduler:       scheduler,
	})
	require.NoError(t, err)
	return manager
}

func stageAndCommit(t *testing.T, manager *Manager, scheduler *controlledScheduler, version int64) {
	t.Helper()
	var staged *Staged
	require.NoError(t, manager.Stage(version, func() bool { return true }, func(s *Staged, err error) {
		require.NoError(t, err)
		staged = s
	}))
	(<-scheduler.pending)()
	require.NoError(t, manager.Commit(staged))
}
