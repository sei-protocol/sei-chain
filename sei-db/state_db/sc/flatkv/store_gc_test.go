package flatkv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

// The GC surface reads the snapshot directory and two plain fields, so most of it can be exercised
// without opening the five PebbleDBs a real store carries. Only the tests that assert against
// snapshots WriteSnapshot produced, or against a concurrent committer, pay for a live store.
func gcStore(t *testing.T, dir string) (*CommitStore, gc.PrunableStore) {
	t.Helper()
	s := &CommitStore{config: config.Config{DataDir: dir}}
	return s, s
}

// gcStoreAtHead is gcStore with a committed head. The head is what the rollback window is resolved
// against, so a test that wants a particular depth sets one rather than passing a height in.
func gcStoreAtHead(t *testing.T, dir string, head int64) (*CommitStore, gc.PrunableStore) {
	t.Helper()
	s, store := gcStore(t, dir)
	s.committedVersion = head
	return s, store
}

// mkSnapshots creates snapshot directories and points "current" at the newest one on disk, which is
// the shape production leaves behind: WriteSnapshot repoints the symlink at each snapshot it writes.
// The GC surface is bounded by the active snapshot, so a test that left "current" unset would be
// measuring a state a live store never reaches. The tests that want a stale or missing symlink say so
// after calling this.
func mkSnapshots(t *testing.T, dir string, versions ...int64) {
	t.Helper()
	for _, v := range versions {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, snapshotName(v)), 0750))
	}
	onDisk := snapshotVersions(t, dir)
	if len(onDisk) == 0 {
		return
	}
	require.NoError(t, updateCurrentSymlink(dir, snapshotName(onDisk[len(onDisk)-1])))
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

// This store's history is the state WAL, which the collector manages as a store in its own right and
// prunes to the shared minimum. So PruneHistory here is a no-op, and must not reach the snapshots
// PruneSnapshots owns.
func TestGCPruneHistoryIsANoOp(t *testing.T) {
	s, store := gcStore(t, t.TempDir())
	dir := s.flatkvDir()
	mkSnapshots(t, dir, 5, 10, 20)

	require.NoError(t, store.PruneHistory(20))
	require.Equal(t, []int64{5, 10, 20}, snapshotVersions(t, dir))
}

// The head is the committed version, not the newest snapshot: it is this store's ingest position, and
// what GetRollbackFloor measures the rollback window against.
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

// snapshotFloor is fed a directory listing that happens to be sorted today, but it must not depend on
// that: choosing the floor by iteration position rather than by value would answer too high on an
// unsorted slice, and too high is the one direction that lets the WAL be pruned past blocks a restore
// replays. Every case runs its blocks in a scrambled order and expects the same answer as the sorted
// one would give.
func TestSnapshotFloorIsOrderIndependent(t *testing.T) {
	const head = 100
	for _, tc := range []struct {
		name           string
		blocks         []uint64
		rollbackWindow uint64
		want           uint64
	}{
		{name: "newest at or below the window", blocks: []uint64{20, 5, 10}, rollbackWindow: 85, want: 10},
		{name: "shortfall answers the oldest", blocks: []uint64{97, 90, 95}, rollbackWindow: 10, want: 90},
		{name: "version 0 is never the floor", blocks: []uint64{30, 0, 10}, rollbackWindow: 95, want: 10},
		{name: "only version 0 reads as none", blocks: []uint64{0}, rollbackWindow: 10, want: 0},
		{name: "window past genesis", blocks: []uint64{40, 10}, rollbackWindow: 100, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, snapshotFloor(tc.blocks, head, tc.rollbackWindow))
		})
	}
}

// Restoring to a height inside the rollback window means starting at the newest snapshot at or below
// that depth and replaying the WAL forward, so that snapshot is the oldest thing this store must keep.
// The depth comes from its own head, which is why every case here fixes a head of 100.
func TestGCRollbackFloorIsNewestSnapshotPastTheWindow(t *testing.T) {
	s, store := gcStoreAtHead(t, t.TempDir(), 100)
	mkSnapshots(t, s.flatkvDir(), 5, 10, 20)

	for _, tc := range []struct {
		rollbackWindow uint64
		want           uint64
		why            string
	}{
		{rollbackWindow: 85, want: 10, why: "newest at or below head - window"},
		{rollbackWindow: 80, want: 20, why: "a snapshot exactly at that depth serves it"},
		{rollbackWindow: 0, want: 20, why: "a window of 0 reaches the newest snapshot"},
		{rollbackWindow: 95, want: 5, why: "the oldest snapshot exactly at that depth"},
	} {
		require.Equal(t, tc.want, store.GetRollbackFloor(tc.rollbackWindow), tc.why)
	}
}

// Whatever the window, the answer is either a snapshot that exists or 0. That is what lets the
// collector hold history at a floor this store can restore from, and it is the property a store must
// never break — a floor naming a height with no snapshot on it would be a restore point the store
// cannot serve.
func TestGCRollbackFloorIsAlwaysAnExistingSnapshot(t *testing.T) {
	const head = 40
	s, store := gcStoreAtHead(t, t.TempDir(), head)
	mkSnapshots(t, s.flatkvDir(), 7, 13, 29)

	for rollbackWindow := uint64(0); rollbackWindow <= 60; rollbackWindow++ {
		if rollbackWindow >= head {
			require.Equal(t, uint64(0), store.GetRollbackFloor(rollbackWindow),
				"a window deeper than the head leaves nothing eligible at rollbackWindow=%d", rollbackWindow)
			continue
		}
		require.Contains(t, []uint64{7, 13, 29}, store.GetRollbackFloor(rollbackWindow),
			"floor is not a snapshot at rollbackWindow=%d", rollbackWindow)
	}
}

// Every snapshot above the window means none can be dropped, and the store reports its oldest — the
// deepest it can actually restore to. That is a snapshot-retention shortfall (the snapshot depth is
// shallower than RollbackWindow), and naming that snapshot is what keeps it replayable rather than
// letting history be pruned out from under it.
func TestGCRollbackFloorAllSnapshotsInsideTheWindow(t *testing.T) {
	s, store := gcStoreAtHead(t, t.TempDir(), 1_000)
	mkSnapshots(t, s.flatkvDir(), 500, 900)

	require.Equal(t, uint64(500), store.GetRollbackFloor(900), "head - window is 100, below both snapshots")
	require.Equal(t, uint64(0), store.GetRollbackFloor(2_000),
		"a window deeper than the head leaves nothing eligible for pruning")
}

// With no snapshot there is none to name, so nothing here is eligible for pruning: a store that
// cannot restore anywhere cannot say which blocks the WAL may drop.
func TestGCRollbackFloorWithoutSnapshots(t *testing.T) {
	s, store := gcStoreAtHead(t, t.TempDir(), 100)
	require.Equal(t, uint64(0), store.GetRollbackFloor(10))

	// A directory that does not exist yet reads the same way.
	require.NoError(t, os.RemoveAll(s.flatkvDir()))
	require.Equal(t, uint64(0), store.GetRollbackFloor(10))

	// So does a store that has committed nothing.
	_, fresh := gcStore(t, t.TempDir())
	require.Equal(t, uint64(0), fresh.GetRollbackFloor(10))
}

// The initial empty snapshot restores to no committed height, so it is not a restore point and cannot
// be a floor. A store holding only that one is read as holding none.
func TestGCRollbackFloorIgnoresInitialSnapshot(t *testing.T) {
	s, store := gcStoreAtHead(t, t.TempDir(), 100)
	mkSnapshots(t, s.flatkvDir(), 0)

	require.Equal(t, uint64(0), store.GetRollbackFloor(10), "version 0 does not count as a snapshot")

	// With a real snapshot present, version 0 still contributes nothing — not even as the oldest.
	mkSnapshots(t, s.flatkvDir(), 30)
	require.Equal(t, uint64(30), store.GetRollbackFloor(0))
	require.Equal(t, uint64(30), store.GetRollbackFloor(90),
		"head - window is 10; the oldest real snapshot answers, not version 0")
}

// Not knowing which snapshots exist means not knowing which blocks are needed to replay from them, and
// the WAL would be pruned on the strength of that answer. So a failed scan reports 0, holding the
// fleet's history where it is.
func TestGCRollbackFloorScanFailureHoldsHistory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "flatkv")
	require.NoError(t, os.WriteFile(dir, []byte("not a directory"), 0600))

	_, store := gcStoreAtHead(t, dir, 100)
	require.Equal(t, uint64(0), store.GetRollbackFloor(10))
}

// PruneSnapshots drops everything strictly below the height it is given, and the height need not
// have a snapshot on it.
func TestGCPruneSnapshotsDeletesBelowTheCutLine(t *testing.T) {
	s, store := gcStoreAtHead(t, t.TempDir(), 30)
	dir := s.flatkvDir()
	mkSnapshots(t, dir, 0, 5, 10, 20, 30)
	require.NoError(t, updateCurrentSymlink(dir, snapshotName(30)))

	// A cut line of 25 has no snapshot on it; 20 is the newest below it and goes with the rest.
	require.NoError(t, store.PruneSnapshots(25))
	require.Equal(t, []int64{30}, snapshotVersions(t, dir))

	// Idempotent: the same cut line twice deletes nothing more.
	require.NoError(t, store.PruneSnapshots(25))
	require.Equal(t, []int64{30}, snapshotVersions(t, dir))
}

// The snapshot the store reported must survive the cycle that follows, which is what makes the
// collector's minimum meaningful: history is held at a floor the store still has the snapshot for.
// The collector's cut line is the minimum across stores and so is at or below this store's own
// answer, which the loop walks the tightest case of.
func TestGCPruneSnapshotsKeepsTheSnapshotItReported(t *testing.T) {
	s, store := gcStoreAtHead(t, t.TempDir(), 40)
	dir := s.flatkvDir()
	mkSnapshots(t, dir, 5, 10, 20, 30)
	require.NoError(t, updateCurrentSymlink(dir, snapshotName(30)))

	for rollbackWindow := uint64(0); rollbackWindow <= 40; rollbackWindow++ {
		floor := store.GetRollbackFloor(rollbackWindow)
		before := snapshotVersions(t, dir)
		require.NoError(t, store.PruneSnapshots(floor))
		remaining := snapshotVersions(t, dir)

		if floor == 0 {
			require.Equal(t, before, remaining,
				"nothing is eligible at rollbackWindow=%d, so nothing may be deleted", rollbackWindow)
			continue
		}
		require.NotEmpty(t, remaining, "a store that had a snapshot must still have one")
		require.Contains(t, remaining, int64(floor), //nolint:gosec // bounded by the loop
			"the reported floor must survive at rollbackWindow=%d", rollbackWindow)
	}
}

// A cut line at or below the oldest snapshot leaves the lot. It must not be read as "delete
// everything below the newest".
func TestGCPruneSnapshotsBelowTheOldestSnapshotIsNoOp(t *testing.T) {
	s, store := gcStoreAtHead(t, t.TempDir(), 1_000)
	dir := s.flatkvDir()
	mkSnapshots(t, dir, 500, 900)
	require.NoError(t, updateCurrentSymlink(dir, snapshotName(900)))

	require.NoError(t, store.PruneSnapshots(500))
	require.Equal(t, []int64{500, 900}, snapshotVersions(t, dir))
}

// The collector never passes 0 — it means nothing is eligible — but the store absorbs it as a no-op
// rather than reading it as an empty range to delete against.
func TestGCPruneSnapshotsAtZeroIsNoOp(t *testing.T) {
	s, store := gcStoreAtHead(t, t.TempDir(), 10)
	dir := s.flatkvDir()
	mkSnapshots(t, dir, 5, 10)
	require.NoError(t, updateCurrentSymlink(dir, snapshotName(10)))

	require.NoError(t, store.PruneSnapshots(0))
	require.Equal(t, []int64{5, 10}, snapshotVersions(t, dir))
}

// A store with no snapshots has nothing to prune, which is not a failure — and notably not an error
// about the missing active symlink, since there is no deletion to protect.
func TestGCPruneSnapshotsWithoutSnapshots(t *testing.T) {
	s, store := gcStoreAtHead(t, t.TempDir(), 100)
	require.NoError(t, store.PruneSnapshots(10))

	require.NoError(t, os.RemoveAll(s.flatkvDir()))
	require.NoError(t, store.PruneSnapshots(10))
}

// The active snapshot is what the next open resolves to, so the cut line stops there even when it is
// asked to go deeper. This is the shape a crash between WriteSnapshot's rename and its symlink update
// leaves behind: snapshot 10 is on disk while "current" is still 5. Deleting 5 would leave a dangling
// symlink, which os.Readlink resolves happily, so the store would open against a directory that is
// not there.
func TestGCPruneSnapshotsKeepsActiveSnapshotBelowTheCutLine(t *testing.T) {
	s, store := gcStoreAtHead(t, t.TempDir(), 10)
	dir := s.flatkvDir()
	mkSnapshots(t, dir, 3, 5, 10)
	require.NoError(t, updateCurrentSymlink(dir, snapshotName(5)))

	require.NoError(t, store.PruneSnapshots(10))
	require.Equal(t, []int64{5, 10}, snapshotVersions(t, dir),
		"the cut line stops at the active snapshot, so 3 goes and 5 stays")
}

// Without a resolvable active snapshot there is nothing to bound the deletion by, so the prune is
// refused rather than run blind.
func TestGCPruneSnapshotsRefusesWithoutActiveSnapshot(t *testing.T) {
	s, store := gcStoreAtHead(t, t.TempDir(), 10)
	dir := s.flatkvDir()
	mkSnapshots(t, dir, 5, 10)
	require.NoError(t, os.Remove(currentPath(dir)))

	require.Error(t, store.PruneSnapshots(10))
	require.Equal(t, []int64{5, 10}, snapshotVersions(t, dir))
}

// The same refusal on the reporting side: a floor above the snapshot this store would resume from
// would let the WAL be pruned past the blocks that resume replays, so an unresolvable "current" holds
// the fleet's history where it is.
func TestGCRollbackFloorHoldsHistoryWithoutActiveSnapshot(t *testing.T) {
	s, store := gcStoreAtHead(t, t.TempDir(), 100)
	dir := s.flatkvDir()
	mkSnapshots(t, dir, 10, 20)
	require.Equal(t, uint64(20), store.GetRollbackFloor(80), "the newest snapshot past the window")

	require.NoError(t, os.Remove(currentPath(dir)))
	require.Equal(t, uint64(0), store.GetRollbackFloor(80))
}

// A snapshot newer than "current" is what a crash between WriteSnapshot's rename and its symlink
// update leaves behind, and the next open takes the symlink rather than adopting the orphan. So the
// floor stops at the active snapshot: reporting the orphan would hold the WAL only from there, and
// the replay that starts at the active snapshot needs the blocks below it.
func TestGCRollbackFloorStopsAtTheActiveSnapshot(t *testing.T) {
	s, store := gcStoreAtHead(t, t.TempDir(), 100)
	dir := s.flatkvDir()
	mkSnapshots(t, dir, 10, 50)
	require.NoError(t, updateCurrentSymlink(dir, snapshotName(10)))

	require.Equal(t, uint64(10), store.GetRollbackFloor(20),
		"head - window is 80, so the orphan at 50 would answer if the active snapshot did not bound it")
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
		pruneSnapshotsByCount(s.ctx, s.snapshotLayout(), 15)
		return snapshotVersions(t, dir)
	}

	require.Equal(t, []int64{15}, run(false),
		"left to itself the count-based pruner takes everything below the current snapshot")
	require.Equal(t, []int64{5, 10, 15}, run(true),
		"under the collector it must not delete the snapshot being held for the rollback window")
}

// pruneSpyWAL is a statewal.StateWAL that records the one call tryTruncateWAL makes on it. The other
// interface methods are never reached on this path, so the embedded nil satisfies the type and panics
// loudly if that ever stops being true.
type pruneSpyWAL struct {
	statewal.StateWAL
	pruned   bool
	prunedTo uint64
}

func (w *pruneSpyWAL) Prune(lowestBlockNumberToKeep uint64) error {
	w.pruned = true
	w.prunedTo = lowestBlockNumberToKeep
	return nil
}

// tryTruncateWAL is the second mechanism ExternalPruning stands down, and by the config doc the
// higher-stakes of the pair: left running under the collector it would truncate the state WAL to
// this store's own earliest snapshot while the collector holds the fleet's floor lower — pruning the
// WAL out from under SS, which replays it. Its sibling pruneSnapshotsByCount is pinned by the test
// above; this pins the WAL half so a later change cannot silently re-enable it.
//
// A real snapshot above version 0 is laid down so tryTruncateWAL has a floor to truncate to (version
// 0 short-circuits it before the guard). The observable is whether it schedules a WAL prune: it must
// with retention its own, and must not under ExternalPruning.
func TestGCExternalPruningStandsDownWALTruncation(t *testing.T) {
	prune := func(external bool) *pruneSpyWAL {
		s, _ := gcStore(t, t.TempDir())
		s.config.ExternalPruning = external
		spy := &pruneSpyWAL{}
		s.wal = spy
		mkSnapshots(t, s.flatkvDir(), 5, 10)

		s.tryTruncateWAL()
		return spy
	}

	self := prune(false)
	require.True(t, self.pruned, "with retention its own, tryTruncateWAL must prune the WAL")
	require.Equal(t, uint64(5), self.prunedTo, "it truncates up to the earliest snapshot")

	require.False(t, prune(true).pruned, "under ExternalPruning tryTruncateWAL must not touch the WAL")
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

	floor := store.GetRollbackFloor(1)
	require.Equal(t, uint64(4), floor, "newest snapshot at or below head - 1")

	require.NoError(t, store.PruneSnapshots(floor))
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
		// No assertion on the floor itself: the committer advances the head between these calls,
		// so any relation to the head read above is racy by construction. What is under test is
		// that reading and pruning concurrently with a committer is safe at all.
		floor := store.GetRollbackFloor(10)
		require.NoError(t, store.PruneSnapshots(floor))
		require.NoError(t, store.PruneHistory(floor))
	}
	<-done
}
