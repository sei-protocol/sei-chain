package flatkv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// The GC surface reads the snapshot directory and two plain fields, so most of it can be exercised
// without opening the five PebbleDBs a real store carries. Only the tests that assert against
// snapshots WriteSnapshot produced, or against a concurrent committer, pay for a live store.
func gcStore(t *testing.T, dir string) (*CommitStore, gc.PrunableStore) {
	t.Helper()
	s := &CommitStore{config: config.Config{DataDir: dir}}
	return s, s
}

func mkSnapshots(t *testing.T, dir string, versions ...int64) {
	t.Helper()
	for _, v := range versions {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, snapshotName(v)), 0750))
	}
}

func snapshotVersions(t *testing.T, dir string) []int64 {
	t.Helper()
	var found []int64
	require.NoError(t, traverseSnapshots(dir, true, func(v int64) (bool, error) {
		found = append(found, v)
		return false, nil
	}))
	return found
}

// A snapshot store asks for no history of its own: 0 is what keeps it inside the collector's shared
// minimum, which is what protects its snapshots. See GetRetentionWindow for why the sentinel that
// reads like "keep more" would do the opposite here.
func TestGCRetentionWindowIsZero(t *testing.T) {
	_, store := gcStore(t, t.TempDir())
	require.Equal(t, int64(0), store.GetRetentionWindow())
}

// The head is the committed version, not the newest snapshot: it is this store's ingest position, and
// the collector takes a minimum over those to find the fleet's head.
func TestGCLatestBlockIsCommittedVersion(t *testing.T) {
	s, store := gcStore(t, t.TempDir())

	latest, err := store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(0), latest, "nothing committed yet")

	// Snapshots do not move the head; commits do.
	mkSnapshots(t, s.flatkvDir(), 10, 20)
	latest, err = store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(0), latest)

	s.committedVersion = 42
	latest, err = store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(42), latest)
}

// Restoring to a height means starting at the newest snapshot at or below it and replaying the WAL
// forward, so that snapshot is the oldest thing this store must keep.
func TestGCPruningBoundaryIsNewestSnapshotAtOrBelowCutLine(t *testing.T) {
	s, store := gcStore(t, t.TempDir())
	mkSnapshots(t, s.flatkvDir(), 5, 10, 20)

	for _, tc := range []struct {
		cutLine uint64
		want    uint64
		why     string
	}{
		{cutLine: 15, want: 10, why: "newest at or below"},
		{cutLine: 20, want: 20, why: "a snapshot exactly on the cut line serves it"},
		{cutLine: 100, want: 20, why: "far above every snapshot"},
		{cutLine: 5, want: 5, why: "the oldest snapshot exactly on the cut line"},
	} {
		require.Equal(t, tc.want, store.GetPruningBoundary(tc.cutLine), tc.why)
	}
}

// The answer must never exceed the cut line: the collector takes a minimum across stores without
// clamping, so a higher answer here would raise pruneHeight above head-RollbackWindow and prune away
// the rollback window every other store is holding.
func TestGCPruningBoundaryNeverExceedsCutLine(t *testing.T) {
	s, store := gcStore(t, t.TempDir())
	mkSnapshots(t, s.flatkvDir(), 7, 13, 29)

	for cutLine := uint64(1); cutLine <= 40; cutLine++ {
		require.LessOrEqual(t, store.GetPruningBoundary(cutLine), cutLine,
			"boundary above cut line at cutLine=%d", cutLine)
	}
}

// Every snapshot above the cut line means none can be dropped, so the store answers the cut line
// rather than its oldest snapshot — holding the fleet back to that snapshot would buy nothing. The
// store genuinely cannot restore to the cut line here; that is a snapshot-retention shortfall, not
// something a lower answer would repair.
func TestGCPruningBoundaryAllSnapshotsAboveCutLine(t *testing.T) {
	s, store := gcStore(t, t.TempDir())
	mkSnapshots(t, s.flatkvDir(), 500, 900)

	require.Equal(t, uint64(100), store.GetPruningBoundary(100))
}

// With no snapshot to restore from, the store stops the cycle rather than dropping out of it: it will
// replay forward once its first snapshot lands, and the WAL holds the range it will replay from.
func TestGCPruningBoundaryWithoutSnapshots(t *testing.T) {
	s, store := gcStore(t, t.TempDir())
	require.Equal(t, gc.CannotServeRollback, store.GetPruningBoundary(100))

	// A directory that does not exist yet reads the same way.
	require.NoError(t, os.RemoveAll(s.flatkvDir()))
	require.Equal(t, gc.CannotServeRollback, store.GetPruningBoundary(100))
}

// The initial empty snapshot restores to no committed height, so reaching any height from it needs the
// WAL from its very first block — which is what CannotServeRollback already means, and is the value 0
// would collide with anyway.
func TestGCPruningBoundaryIgnoresInitialSnapshot(t *testing.T) {
	s, store := gcStore(t, t.TempDir())
	mkSnapshots(t, s.flatkvDir(), 0)

	require.Equal(t, gc.CannotServeRollback, store.GetPruningBoundary(100))

	// With a real snapshot present, version 0 still contributes nothing.
	mkSnapshots(t, s.flatkvDir(), 30)
	require.Equal(t, uint64(30), store.GetPruningBoundary(100))
	require.Equal(t, uint64(10), store.GetPruningBoundary(10), "not 0, and not the initial snapshot")
}

// Not knowing which snapshots exist means not knowing which blocks are needed to replay from them, and
// the WAL would be pruned on the strength of that answer. So a failed scan blocks the cycle.
func TestGCPruningBoundaryScanFailureBlocksTheCycle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "flatkv")
	require.NoError(t, os.WriteFile(dir, []byte("not a directory"), 0600))

	_, store := gcStore(t, dir)
	require.Equal(t, gc.CannotServeRollback, store.GetPruningBoundary(100))
}

// PruneBelow drops every snapshot under the floor. The floor is a minimum across stores and so never
// exceeds the boundary this store reported, which is why the snapshot it named always survives.
func TestGCPruneBelowDeletesSnapshotsBelowFloor(t *testing.T) {
	s, store := gcStore(t, t.TempDir())
	dir := s.flatkvDir()
	mkSnapshots(t, dir, 0, 5, 10, 20, 30)
	require.NoError(t, updateCurrentSymlink(dir, snapshotName(30)))

	require.NoError(t, store.PruneBelow(20))
	require.Equal(t, []int64{20, 30}, snapshotVersions(t, dir))

	// Idempotent: the same floor twice deletes nothing more.
	require.NoError(t, store.PruneBelow(20))
	require.Equal(t, []int64{20, 30}, snapshotVersions(t, dir))
}

// A floor of 0 is the collector's "nothing to do" and must not be read as "delete everything below
// any height".
func TestGCPruneBelowZeroIsNoOp(t *testing.T) {
	s, store := gcStore(t, t.TempDir())
	dir := s.flatkvDir()
	mkSnapshots(t, dir, 5, 10)
	require.NoError(t, updateCurrentSymlink(dir, snapshotName(10)))

	require.NoError(t, store.PruneBelow(0))
	require.Equal(t, []int64{5, 10}, snapshotVersions(t, dir))
}

// The active snapshot is what the next open resolves to, so it survives however deep the request. The
// contract should never produce such a request; this pins that a wrong answer elsewhere cannot leave
// the store unbootable here.
func TestGCPruneBelowKeepsActiveSnapshot(t *testing.T) {
	s, store := gcStore(t, t.TempDir())
	dir := s.flatkvDir()
	mkSnapshots(t, dir, 5, 10)
	require.NoError(t, updateCurrentSymlink(dir, snapshotName(5)))

	require.NoError(t, store.PruneBelow(1_000))
	require.Equal(t, []int64{5}, snapshotVersions(t, dir))
}

// Without a resolvable active snapshot there is nothing to protect the deletion against, so the prune
// is refused rather than run blind.
func TestGCPruneBelowRefusesWithoutActiveSnapshot(t *testing.T) {
	s, store := gcStore(t, t.TempDir())
	dir := s.flatkvDir()
	mkSnapshots(t, dir, 5, 10)

	require.Error(t, store.PruneBelow(10))
	require.Equal(t, []int64{5, 10}, snapshotVersions(t, dir))
}

// ExternalPruning is off unless asked for: a store built without a collector must keep pruning
// itself, since standing down with nothing to replace it grows snapshots without bound.
func TestGCExternalPruningDefaultsOff(t *testing.T) {
	_, store := gcStore(t, t.TempDir())
	require.False(t, store.ExternalPruning())

	s := &CommitStore{config: config.Config{DataDir: t.TempDir(), ExternalPruning: true}}
	require.True(t, s.ExternalPruning())
}

// The guarantee the flag exists for: the count-based pruner and the collector are never both
// enforcing retention, because both read this one field. SnapshotKeepRecent 0 is the most
// destructive setting there is — delete every snapshot but the current one — so if anything can
// defeat the stand-down, it shows here.
func TestGCExternalPruningStandsDownSnapshotPruner(t *testing.T) {
	run := func(external bool) []int64 {
		dir := t.TempDir()
		s := &CommitStore{
			ctx: t.Context(),
			config: config.Config{
				DataDir:            dir,
				SnapshotKeepRecent: 0,
				ExternalPruning:    external,
			},
		}
		mkSnapshots(t, dir, 5, 10, 15)
		s.pruneSnapshots(dir, 15)
		return snapshotVersions(t, dir)
	}

	require.Equal(t, []int64{15}, run(false),
		"left to itself the count-based pruner takes everything below the current snapshot")
	require.Equal(t, []int64{5, 10, 15}, run(true),
		"under the collector it must not delete the snapshot being held for the rollback window")
}

// End to end against snapshots WriteSnapshot actually produced, including that the store still opens
// afterwards — the prune must not disturb what the next open resolves through.
func TestGCPrunesRealSnapshotsAndStoreStillOpens(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 2
	// Configured the way a collector-managed store actually is. SnapshotKeepRecent is deliberately
	// left at its default of 1 to show ExternalPruning overrides it rather than cooperating with it:
	// under that count, snapshots 0 and 2 would be gone before the collector ever saw them.
	cfg.ExternalPruning = true

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())

	for range 6 {
		commitAndCheck(t, s)
	}
	// A fresh store starts at the initial snapshot; the interval adds one every other block.
	require.Equal(t, []int64{0, 2, 4, 6}, snapshotVersions(t, cfg.DataDir))

	store, ok := any(s).(gc.PrunableStore)
	require.True(t, ok, "a FlatKV commit store must satisfy gc.PrunableStore")

	latest, err := store.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(6), latest)

	boundary := store.GetPruningBoundary(5)
	require.Equal(t, uint64(4), boundary, "newest snapshot at or below the cut line")

	require.NoError(t, store.PruneBelow(boundary))
	require.Equal(t, []int64{4, 6}, snapshotVersions(t, cfg.DataDir))

	require.NoError(t, s.Close())

	cfg2 := config.DefaultTestConfig(t)
	cfg2.DataDir = cfg.DataDir
	s2, err := newCommitStoreWithWAL(t.Context(), cfg2)
	require.NoError(t, err)
	require.NoError(t, s2.LoadLatest())
	require.Equal(t, int64(6), s2.Version(), "the pruned store reopens at its committed version")
	require.NoError(t, s2.Close())
}

// The collector runs on its own goroutine while the store commits on another. Only the race detector
// can judge this: CommitStore keeps its committed version in a plain field that Commit advances under
// the write lock, and WriteSnapshot rewrites the snapshot directory the other two methods scan.
func TestGCConcurrentWithCommitter(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	cfg.SnapshotInterval = 5
	cfg.SnapshotKeepRecent = 1_000 // the collector is the only deleter here

	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
	defer func() { require.NoError(t, s.Close()) }()

	store := gc.PrunableStore(s)

	const blocks = 30
	done := make(chan struct{})
	go func() {
		defer close(done)
		for version := int64(1); version <= blocks; version++ {
			// Assertions are not allowed off the main test goroutine; fail loudly instead.
			if _, err := s.Commit(version); err != nil {
				panic(err)
			}
		}
	}()

	// Drive prune cycles exactly as the collector does, off the committer's goroutine.
	for range blocks {
		head, err := store.GetLatestBlock()
		require.NoError(t, err)
		require.LessOrEqual(t, head, uint64(blocks))
		if head <= 10 {
			continue
		}
		boundary := store.GetPruningBoundary(head - 10)
		require.LessOrEqual(t, boundary, head-10, "boundary must never exceed the cut line")
		if boundary != gc.CannotServeRollback {
			require.NoError(t, store.PruneBelow(boundary))
		}
	}
	<-done
}
