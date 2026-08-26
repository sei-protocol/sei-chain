package flatkv

import (
	"sync"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/stretchr/testify/require"
)

// scheduledOnlyStore returns a store whose only source of snapshots is a dispatched checkpoint target.
// The periodic interval is off so a snapshot appearing is attributable to the scheduled path.
func scheduledOnlyStore(t *testing.T) *CommitStore {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 0
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// The target names the block the snapshot is taken at, not "soon": the blocks before it pass without
// one, and the snapshot lands on exactly the version that was asked for.
func TestScheduledCheckpointSnapshotsAtTheTargetVersion(t *testing.T) {
	s := scheduledOnlyStore(t)

	s.ScheduleCheckpoint(3)
	require.Equal(t, int64(3), s.pendingCheckpoint.Load())

	commitAndCheck(t, s)
	commitAndCheck(t, s)
	require.Equal(t, int64(3), s.pendingCheckpoint.Load(), "the target block has not arrived yet")
	require.NotContains(t, snapshotVersions(t, s.flatkvDir()), int64(2))

	commitAndCheck(t, s)
	require.Zero(t, s.pendingCheckpoint.Load())
	require.Contains(t, snapshotVersions(t, s.flatkvDir()), int64(3))
}

// A target the store has already passed is ignored: accepting one would snapshot a height Commit
// will never see again.
func TestScheduleCheckpointIgnoresATargetAtOrBelowTheCommittedVersion(t *testing.T) {
	s := scheduledOnlyStore(t)
	commitAndCheck(t, s)

	s.ScheduleCheckpoint(1)
	s.ScheduleCheckpoint(0)
	require.Zero(t, s.pendingCheckpoint.Load())
}

func TestScheduleCheckpointIgnoresASecondTargetWhileOneIsPending(t *testing.T) {
	s := scheduledOnlyStore(t)

	s.ScheduleCheckpoint(5)
	s.ScheduleCheckpoint(6)
	require.Equal(t, int64(5), s.pendingCheckpoint.Load())
}

func TestLatestVersionReportsTheCommittedVersion(t *testing.T) {
	s := scheduledOnlyStore(t)

	require.Equal(t, int64(0), s.LatestVersion())
	commitAndCheck(t, s)
	require.Equal(t, int64(1), s.LatestVersion())
}

// A scheduler queries these three methods from its own goroutine while the commit path holds the
// store's write lock across a snapshot. This runs the two against each other to pin the lock order;
// the test completing is the assertion, because the failure it looks for is a wedged commit loop.
//
// The querying is done directly rather than by running a real scheduler, which makes it both denser
// than the scheduler's poll interval and independent of it. Which versions end up snapshotted depends
// on when a query lands, so the only thing asserted about the result is the invariant that holds
// regardless: every snapshot sits on an interval boundary.
func TestScheduleCheckpointQueriesConcurrentWithCommits(t *testing.T) {
	const interval = 4

	s := scheduledOnlyStore(t)

	stop := make(chan struct{})
	var polling sync.WaitGroup
	polling.Add(1)
	go func() {
		defer polling.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if !s.CheckpointInProgress() {
				version := s.LatestVersion()
				s.ScheduleCheckpoint((version/interval + 1) * interval)
			}
			time.Sleep(50 * time.Microsecond)
		}
	}()

	for i := 0; i < 20; i++ {
		commitAndCheck(t, s)
	}
	close(stop)
	polling.Wait()

	for _, version := range snapshotVersions(t, s.flatkvDir()) {
		if version == 0 {
			continue // the empty snapshot a fresh store is initialized with
		}
		require.Zero(t, version%interval, "snapshot %d is not on an interval boundary", version)
	}
}
