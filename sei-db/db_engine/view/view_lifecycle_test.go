package view

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestViewFinalizeThenReleaseHappyPath(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 1, 4096)
	view, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, view.Finalize(hashWrites(testHash)))
	require.NoError(t, view.Release())
}

// A consumer with nothing to record still has to finalize, so an empty write set is legal: it is
// finalization, not the metadata, that makes a view flushable.
func TestViewFinalizeWithNoWritesIsLegal(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 1, 4096)
	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit()
	require.NoError(t, err)

	require.NoError(t, view.Finalize(nil))
	awaitFlushed(t, view, time.Second)
	require.NoError(t, view.Release())

	db := manager.(*viewManager).db.(*testDB)
	val, ok := db.get("k")
	require.True(t, ok, "an empty finalization must still let the diff flush")
	require.Equal(t, []byte("v"), val)
}

func TestViewFinalizeTwiceFails(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 1, 4096)
	view, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, view.Finalize(hashWrites(testHash)))
	require.Error(t, view.Finalize(hashWrites(testHash)))
	require.NoError(t, view.Release())
}

func TestViewReleaseWithoutFinalizeIsFatal(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 1, 4096)
	view, err := manager.Commit()
	require.NoError(t, err)
	require.Error(t, view.Release(),
		"releasing the final reservation on an unfinalized view must fail")
}

func TestViewDoubleReleaseFails(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 1, 4096)
	view, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, view.Finalize(hashWrites(testHash)))
	require.NoError(t, view.Release())
	require.Error(t, view.Release())
}

func TestViewReserveExtendsLifetime(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 1, 4096)
	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit()
	require.NoError(t, err)

	require.NoError(t, view.Reserve()) // refCount = 2
	require.NoError(t, view.Finalize(hashWrites(testHash)))
	require.NoError(t, view.Release()) // refCount = 1, still alive

	val, found, err := view.Get([]byte("k"), false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), val)

	require.NoError(t, view.Release()) // refCount = 0
}

func TestViewFinalizeGatesFlush(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 1, 4096)
	db := manager.(*viewManager).db.(*testDB)

	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit()
	require.NoError(t, err)

	// Without Finalize, nothing may flush.
	require.Never(t, func() bool { return db.has("k") }, 40*time.Millisecond, 5*time.Millisecond,
		"unfinalized view must not flush")

	require.NoError(t, view.Finalize(hashWrites(testHash)))
	awaitFlushed(t, view, time.Second)
	require.NoError(t, view.Release())

	val, ok := db.get("k")
	require.True(t, ok)
	require.Equal(t, []byte("v"), val)
}

func TestViewAwaitFlushContextCancelled(t *testing.T) {
	db := newTestDB(nil)
	db.commitBlock = make(chan struct{})
	defer close(db.commitBlock)

	manager := newTestManagerWithDB(t, db, 1, 4096)
	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, view.Finalize(hashWrites(testHash)))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	require.Error(t, view.AwaitFlush(ctx), "flush is stalled, so AwaitFlush must time out")

	require.NoError(t, view.Release())
}

func TestAwaitFlushRetiredVersionWithCancelledCtx(t *testing.T) {
	manager, _ := newTestManager(t, nil, 1, 1<<20)
	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit()
	require.NoError(t, err)
	ver := view.(*viewImpl).version
	finalizeAndRelease(t, view)
	awaitRetired(t, manager, ver)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, view.AwaitFlush(ctx),
		"a retired (untracked) version reports an error even though its flush completed")
}

func TestBackpressureBlocksAndUnblocksOnFlush(t *testing.T) {
	db := newTestDB(nil)
	db.commitBlock = make(chan struct{})

	cfg := newTestConfig(1, 4096)
	cfg.MaxUnflushedVersions = 2
	manager := newTestManagerWithConfig(t, cfg, db)

	// Accumulate more unflushed-but-eligible versions than MaxUnflushedVersions (flush is stalled).
	for i := 0; i < 3; i++ {
		commitFinalizeRelease(t, manager)
	}

	blocked := make(chan struct{})
	go func() {
		view, err := manager.Commit()
		require.NoError(t, err)
		finalizeAndRelease(t, view)
		close(blocked)
	}()

	select {
	case <-blocked:
		close(db.commitBlock)
		t.Fatal("Commit() should have blocked on backpressure")
	case <-time.After(50 * time.Millisecond):
	}

	// Let the flusher drain; backpressure must release.
	close(db.commitBlock)
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("Commit() did not unblock after flush drained")
	}
}

// A long-held reservation must not convert release-lag into Commit backpressure: versions blocked
// behind a still-referenced view are not flushable, so they must not count toward
// MaxUnflushedVersions. Regression test: the flush-eligibility scan used to count them, blocking
// Commit indefinitely while the underlying DB sat idle.
func TestHeldReservationDoesNotTriggerCommitBackpressure(t *testing.T) {
	db := newTestDB(nil)
	cfg := newTestConfig(1, 4096)
	cfg.MaxUnflushedVersions = 2
	manager := newTestManagerWithConfig(t, cfg, db)

	// v1 is finalized and flushed but never released: it cannot retire, so no later version can
	// flush until it is released.
	require.NoError(t, manager.Set([]byte("k1"), []byte("v1")))
	view1, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, view1.Finalize(hashWrites(testHash)))
	awaitFlushed(t, view1, 2*time.Second)

	// Accumulate well more than MaxUnflushedVersions finalized-and-released versions behind the
	// held view. None of them are flushable, so none may count toward backpressure and no
	// Commit may block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ {
			commitFinalizeRelease(t, manager)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Commit blocked on backpressure while flushing was stalled on a held reservation")
	}

	// Releasing the blocker unblocks the pipeline: everything behind it flushes and retires.
	require.NoError(t, view1.Release())
	awaitRetired(t, manager, 6)
	require.True(t, db.has("k1"))
}

// The manager owns the database it was constructed with and closes it, so that nothing can keep reading
// or writing a database whose staging and cache have gone away. The pools stay open: they are shared
// with other managers and belong to the caller.
func TestCloseClosesOwnedDBAndLeavesPoolsOpen(t *testing.T) {
	db := newTestDB(nil)
	manager := newTestManagerWithDB(t, db, 1, 4096)
	require.NoError(t, manager.Close())
	require.True(t, db.isClosed(), "the manager owns the DB and must close it")
}

func TestCloseDoesNotFlush(t *testing.T) {
	db := newTestDB(nil)
	manager := newTestManagerWithDB(t, db, 1, 4096)

	// v1 is never finalized, which deterministically keeps the background flusher away from v2:
	// the flush frontier stops at the first unfinalized version.
	require.NoError(t, manager.Set([]byte("k1"), []byte("v1")))
	_, err := manager.Commit()
	require.NoError(t, err)

	require.NoError(t, manager.Set([]byte("k2"), []byte("v2")))
	view2, err := manager.Commit()
	require.NoError(t, err)
	finalizeAndRelease(t, view2)

	// Close abandons everything unflushed — finalized or not. Recovery is the upstream WAL's job.
	require.NoError(t, manager.Close())
	require.False(t, db.has("k1"), "Close must not flush unfinalized views")
	require.False(t, db.has("k2"), "Close must not flush finalized views either")
}

func TestCloseIsIdempotent(t *testing.T) {
	db := newTestDB(nil)
	manager := newTestManagerWithDB(t, db, 1, 4096)
	require.NoError(t, manager.Close())
	require.NoError(t, manager.Close(), "a second Close must be a safe no-op")
}

func TestCloseSkipsUnfinalizedView(t *testing.T) {
	db := newTestDB(nil)
	manager := newTestManagerWithDB(t, db, 1, 4096)
	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	_, err := manager.Commit()
	require.NoError(t, err)
	// Never finalized.
	require.NoError(t, manager.Close())
	require.False(t, db.has("k"), "Close must not flush unfinalized views")
}
