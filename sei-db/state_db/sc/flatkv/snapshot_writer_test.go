package flatkv

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
)

// The bulk of this package's suite reaches the writer through commitAndCheck, which flushes it so a
// test can look at the snapshot tree straight after committing. These tests are the ones that exercise
// the writer's own goroutine, so they build a writer directly over stubs rather than going through a
// store.

var _ view.View = (*fakeView)(nil)

// fakeView is a view whose flush and hand-back outcomes the test chooses, and which counts both. The
// methods a SnapshotWriter never reaches panic, so a use this stub was not written for is loud rather
// than silently wrong.
type fakeView struct {
	// Reported by Name.
	name string

	// Returned by AwaitFlush.
	awaitFlushErr error

	// Returned by Reserve. A non-nil value also suppresses the reserve count.
	reserveErr error

	// Counts successful Reserve calls.
	reserves atomic.Int64

	// Counts Release calls.
	releases atomic.Int64
}

func (v *fakeView) Name() string { return v.name }

func (v *fakeView) AwaitFlush(context.Context) error { return v.awaitFlushErr }

func (v *fakeView) Reserve() error {
	if v.reserveErr != nil {
		return v.reserveErr
	}
	v.reserves.Add(1)
	return nil
}

func (v *fakeView) Release() error {
	v.releases.Add(1)
	return nil
}

func (v *fakeView) Get([]byte, bool) ([]byte, bool, error) {
	panic("fakeView: unexpected Get")
}

func (v *fakeView) BatchGet([][]byte) (map[string][]byte, error) {
	panic("fakeView: unexpected BatchGet")
}

func (v *fakeView) GetDiff() (map[string][]byte, error) {
	panic("fakeView: unexpected GetDiff")
}

func (v *fakeView) Finalize([]*proto.KVPair) error {
	panic("fakeView: unexpected Finalize")
}

// fakeViews returns a store view at version backed by one stub per database, as a commit would hand
// to the writer, alongside the stubs so a test can inspect what the writer did to them.
func fakeViews(t *testing.T, version int64) (*storeView, map[string]*fakeView) {
	t.Helper()
	stubs := make(map[string]*fakeView, len(dataDBDirs))
	for _, name := range dataDBDirs {
		stubs[name] = &fakeView{name: name}
	}
	blockView, err := newStoreView(version,
		stubs[accountDBDir], stubs[codeDBDir], stubs[storageDBDir], stubs[miscDBDir])
	require.NoError(t, err)
	return blockView, stubs
}

// requireAllReleased asserts the writer handed back every reservation it took. A reservation left held
// stalls its database's flushes forever, so this is the invariant every path must preserve.
func requireAllReleased(t *testing.T, stubs map[string]*fakeView) {
	t.Helper()
	for name, stub := range stubs {
		require.NotZero(t, stub.reserves.Load(), "%s: the writer must take its own reservation", name)
		require.Equal(t, stub.reserves.Load(), stub.releases.Load(),
			"%s: the writer must hand back every reservation it took", name)
	}
}

var _ types.Checkpointable = (*fakeCheckpointDB)(nil)

// fakeCheckpointDB is a Checkpointable whose Checkpoint the test controls: it announces that it has
// started, then blocks until released, then optionally fails.
type fakeCheckpointDB struct {
	// Closed the first time Checkpoint is called.
	started chan struct{}

	// Checkpoint does not return until this is closed. Nil means do not block.
	release chan struct{}

	// Returned by Checkpoint.
	err error

	// Guards started against the concurrent checkpoints of the four databases.
	once sync.Once
}

func (d *fakeCheckpointDB) Checkpoint(destDir string) error {
	d.once.Do(func() { close(d.started) })
	if d.release != nil {
		<-d.release
	}
	if d.err != nil {
		return d.err
	}
	return os.MkdirAll(destDir, 0o750)
}

// fakeCheckpointDBs returns the same controllable handle for every database, so a single release
// channel gates the whole checkpoint.
func fakeCheckpointDBs(db *fakeCheckpointDB) map[string]types.Checkpointable {
	dbs := make(map[string]types.Checkpointable, len(dataDBDirs))
	for _, name := range dataDBDirs {
		dbs[name] = db
	}
	return dbs
}

// newTestWriter builds a writer over stubs, writing into a temp dir.
func newTestWriter(t *testing.T, interval uint32, queueDepth uint32, db *fakeCheckpointDB) *SnapshotWriter {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, workingDirName), 0o750))
	return newSnapshotWriter(t.Context(), dir, 0, false, interval, queueDepth, fakeCheckpointDBs(db))
}

// newCollectorPrunedTestWriter is newTestWriter with retention handed to the StorageGarbageCollector,
// so the only deletions in the writer's snapshot tree are the ones a cut line asks for. The prune
// tests fix snapshots on disk and would otherwise watch the writer's own count-based pruner remove
// them.
func newCollectorPrunedTestWriter(
	t *testing.T,
	interval uint32,
	queueDepth uint32,
	db *fakeCheckpointDB,
) *SnapshotWriter {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, workingDirName), 0o750))
	return newSnapshotWriter(t.Context(), dir, 0, true, interval, queueDepth, fakeCheckpointDBs(db))
}

// requireBlocked asserts a call has not returned yet.
func requireBlocked(t *testing.T, returned <-chan error, what string) {
	t.Helper()
	select {
	case err := <-returned:
		t.Fatalf("%s returned early: %v", what, err)
	case <-time.After(100 * time.Millisecond):
	}
}

// requireReturns waits for a call to return and yields its error.
func requireReturns(t *testing.T, returned <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-returned:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("%s never returned", what)
		return nil
	}
}

// The queue's depth is the whole of the writer's backpressure: blocks pile up behind a snapshot that is
// still being written, and once the queue is full offering another one waits. That pause is deliberate —
// a snapshot holds the databases pinned, so the blocks behind it are held in memory.
func TestSnapshotWriterBlocksOnceQueueIsFull(t *testing.T) {
	db := &fakeCheckpointDB{started: make(chan struct{}), release: make(chan struct{})}
	w := newTestWriter(t, 1, 1, db)

	first, firstStubs := fakeViews(t, 1)
	require.NoError(t, w.Offer(first))
	<-db.started // taken off the queue, and will not finish until released

	queued, queuedStubs := fakeViews(t, 2)
	require.NoError(t, w.Offer(queued), "the single queue slot is free")

	blocked, blockedStubs := fakeViews(t, 3)
	returned := make(chan error, 1)
	go func() { returned <- w.Offer(blocked) }()
	requireBlocked(t, returned, "Offer with a full queue")

	close(db.release)
	require.NoError(t, requireReturns(t, returned, "Offer after the queue drained"))

	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())
	for _, stubs := range []map[string]*fakeView{firstStubs, queuedStubs, blockedStubs} {
		requireAllReleased(t, stubs)
	}
}

// A commit blocked on a full queue must be released when the writer is closed underneath it. Nothing
// else wakes it: the snapshot it is queued behind holds the databases pinned, and teardown is what tells
// it that wait will never be satisfied. Getting this wrong deadlocks shutdown against block production
// rather than failing.
func TestSnapshotWriterCloseWakesBlockedOffer(t *testing.T) {
	db := &fakeCheckpointDB{started: make(chan struct{}), release: make(chan struct{})}
	w := newTestWriter(t, 1, 1, db)

	first, _ := fakeViews(t, 1)
	require.NoError(t, w.Offer(first))
	<-db.started

	queued, _ := fakeViews(t, 2)
	require.NoError(t, w.Offer(queued))

	blocked, blockedStubs := fakeViews(t, 3)
	offered := make(chan error, 1)
	go func() { offered <- w.Offer(blocked) }()
	requireBlocked(t, offered, "Offer with a full queue")

	closed := make(chan error, 1)
	go func() { closed <- w.Close() }()

	err := requireReturns(t, offered, "Offer after Close")
	require.ErrorIs(t, err, ErrSnapshotWriterClosed,
		"a blocked commit must be told the writer stopped rather than waiting forever")
	requireAllReleased(t, blockedStubs)

	close(db.release)
	require.NoError(t, requireReturns(t, closed, "Close"))
}

// Close waits for an in-flight checkpoint rather than abandoning it, because that checkpoint holds
// handles to databases the caller is about to close. Whatever is still queued behind it is discarded,
// with its reservations handed back.
func TestSnapshotWriterCloseWaitsForCheckpointAndDiscardsQueue(t *testing.T) {
	db := &fakeCheckpointDB{started: make(chan struct{}), release: make(chan struct{})}
	w := newTestWriter(t, 1, 4, db)

	first, firstStubs := fakeViews(t, 7)
	require.NoError(t, w.Offer(first))
	<-db.started

	queued, queuedStubs := fakeViews(t, 8)
	require.NoError(t, w.Offer(queued))

	closed := make(chan error, 1)
	go func() { closed <- w.Close() }()
	requireBlocked(t, closed, "Close while a checkpoint was reading the databases")

	close(db.release)
	require.NoError(t, requireReturns(t, closed, "Close"))
	requireAllReleased(t, firstStubs)
	requireAllReleased(t, queuedStubs)
}

// A block the cadence does not select is handed back unwritten, by the goroutine rather than the
// caller. Flush is how a test observes that the goroutine has got that far.
func TestSnapshotWriterReleasesBlocksItDoesNotSnapshot(t *testing.T) {
	db := &fakeCheckpointDB{started: make(chan struct{})}
	w := newTestWriter(t, 10, 8, db)
	defer func() { require.NoError(t, w.Close()) }()

	all := make([]map[string]*fakeView, 0, 3)
	for version := int64(1); version <= 3; version++ {
		views, stubs := fakeViews(t, version)
		require.NoError(t, w.Offer(views))
		all = append(all, stubs)
	}

	require.NoError(t, w.Flush())
	for _, stubs := range all {
		requireAllReleased(t, stubs)
	}
}

// The first failure is latched and reported by every later call. Bricking also stops the writer, which
// is what makes the failure an error rather than a hang: with the goroutine gone nothing drains the
// queue, so a caller would otherwise block on it forever.
func TestSnapshotWriterBrickStopsWriterAndReportsToEveryCaller(t *testing.T) {
	failure := errors.New("checkpoint exploded")
	db := &fakeCheckpointDB{started: make(chan struct{}), err: failure}
	w := newTestWriter(t, 1, 1, db)

	views, stubs := fakeViews(t, 1)
	require.NoError(t, w.Offer(views))

	require.Eventually(t, func() bool {
		return errors.Is(w.Flush(), failure)
	}, 5*time.Second, 5*time.Millisecond, "the failure must be latched and reported")
	requireAllReleased(t, stubs)

	later, laterStubs := fakeViews(t, 2)
	require.ErrorIs(t, w.Offer(later), failure,
		"Offer is on the commit path, so it must surface the failure rather than block on a dead queue")
	requireAllReleased(t, laterStubs)

	require.ErrorIs(t, w.Flush(), failure)
	require.ErrorIs(t, w.Close(), failure, "Close reports what went wrong rather than hiding it")
}

// The collector's cut line is acted on by the writer's goroutine, not the collector's. With a
// checkpoint in flight the writer cannot reach it, so PruneBelow returns with the snapshots still on
// disk; they go once the writer is free. This is what makes the writer the only mutator of the
// snapshot tree, which is the whole point of routing deletion through it.
func TestSnapshotWriterPruneWaitsForTheWritersGoroutine(t *testing.T) {
	db := &fakeCheckpointDB{started: make(chan struct{}), release: make(chan struct{})}
	w := newCollectorPrunedTestWriter(t, 1, 1, db)
	mkSnapshots(t, w.dir, 5, 10, 20)

	views, stubs := fakeViews(t, 100)
	require.NoError(t, w.Offer(views))
	<-db.started // the goroutine is inside the checkpoint and cannot service a cut line

	require.NoError(t, w.PruneBelow(20))
	require.Equal(t, []int64{5, 10, 20}, snapshotVersions(t, w.dir),
		"nothing may be deleted on the collector's goroutine")

	close(db.release)
	require.Eventually(t, func() bool {
		return slices.Equal([]int64{20, 100}, snapshotVersions(t, w.dir))
	}, 5*time.Second, 10*time.Millisecond, "the writer deletes once it is free")

	require.NoError(t, w.Close())
	requireAllReleased(t, stubs)
}

// Cut lines only ever rise, so one still waiting carries nothing the newer one does not. The cell
// holds exactly one, and it is the newest.
func TestSnapshotWriterPruneCutLineIsSuperseded(t *testing.T) {
	db := &fakeCheckpointDB{started: make(chan struct{}), release: make(chan struct{})}
	w := newCollectorPrunedTestWriter(t, 1, 1, db)

	views, stubs := fakeViews(t, 100)
	require.NoError(t, w.Offer(views))
	<-db.started // nothing drains the cell while the checkpoint runs

	require.NoError(t, w.PruneBelow(6))
	require.NoError(t, w.PruneBelow(11))
	require.Len(t, w.pruneCutLine, 1, "a waiting cut line is replaced, not queued behind")
	require.Equal(t, uint64(11), <-w.pruneCutLine, "the newest cut line is the one kept")

	close(db.release)
	require.NoError(t, w.Close())
	requireAllReleased(t, stubs)
}

// A deletion that fails stops the writer, as a failed checkpoint does. Here "current" is missing, so
// there is nothing to bound the cut line by and the prune refuses rather than running blind.
func TestSnapshotWriterPruneFailureBricks(t *testing.T) {
	w := newCollectorPrunedTestWriter(t, 0, 1, &fakeCheckpointDB{started: make(chan struct{})})
	mkSnapshots(t, w.dir, 5, 10)
	require.NoError(t, os.Remove(currentPath(w.dir)))

	require.NoError(t, w.PruneBelow(10), "the refusal happens on the writer, not at the hand-off")
	require.Eventually(t, func() bool {
		return w.errorIfBricked() != nil
	}, 5*time.Second, 10*time.Millisecond)

	require.ErrorContains(t, w.Close(), "resolve active snapshot")
	require.Equal(t, []int64{5, 10}, snapshotVersions(t, w.dir), "a refused prune deletes nothing")
}

// End to end through a store with the writer running asynchronously: the snapshot appears once
// FlushSnapshots says the writer has caught up, and names the height that was committed.
func TestStoreWritesSnapshotAsynchronously(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 2
	cfg.MaxSnapshotLagBlocks = 1000 // asynchronous, unlike the rest of the suite
	s := setupTestStoreWithConfig(t, cfg)
	defer func() { _ = s.Close() }()

	commitStorageEntry(t, s, ktype.Address{0x01}, ktype.Slot{0x01}, []byte{0xaa})
	commitStorageEntry(t, s, ktype.Address{0x02}, ktype.Slot{0x02}, []byte{0xbb})

	require.NoError(t, s.FlushSnapshots())

	dir := s.flatkvDir()
	for _, sub := range dataDBDirs {
		info, err := os.Stat(filepath.Join(dir, snapshotName(2), sub))
		require.NoError(t, err, "%s should exist in the asynchronously written snapshot", sub)
		require.True(t, info.IsDir())
	}

	target, err := os.Readlink(currentPath(dir))
	require.NoError(t, err)
	require.Equal(t, snapshotName(2), target, "current must point at the snapshot the writer published")
}
