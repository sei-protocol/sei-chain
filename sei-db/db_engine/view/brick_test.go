package view

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A flush failure must brick the manager cleanly: no panic, blocking methods unblock, and methods
// that observe the shutdown report an error wrapping the underlying cause.

func TestFlushFailureBricksManagerCleanly(t *testing.T) {
	db := newTestDB(nil)
	db.commitErr = errors.New("disk full")
	manager := newTestManagerWithDB(t, db, 1, 4096)
	e := manager.(*viewManager)

	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view1, err := manager.Commit()
	require.NoError(t, err)

	// A second view that is never finalized, so it can never flush; its AwaitFlush waiter must be
	// released by the brick rather than hang.
	view2, err := manager.Commit()
	require.NoError(t, err)
	stalledFlushErr := make(chan error, 1)
	go func() {
		stalledFlushErr <- view2.AwaitFlush(context.Background())
	}()

	// Make view1 flush-eligible; the flush attempt fails and bricks the manager.
	require.NoError(t, view1.Finalize(hashWrites(testHash)))
	require.NoError(t, view1.Release())

	select {
	case <-e.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("manager context was not cancelled after the flush failure")
	}

	_, err = manager.Commit()
	require.ErrorContains(t, err, "disk full", "View must report the underlying flush error")

	err = view1.AwaitFlush(context.Background())
	require.ErrorContains(t, err, "disk full", "AwaitFlush must report the underlying flush error")

	select {
	case flushErr := <-stalledFlushErr:
		require.Error(t, flushErr, "an unfinalized view's AwaitFlush must fail once the manager is down")
	case <-time.After(2 * time.Second):
		t.Fatal("AwaitFlush waiter did not unblock after the brick")
	}
}

func TestCloseAfterBrickReportsFatalError(t *testing.T) {
	db := newTestDB(nil)
	db.commitErr = errors.New("disk full")
	manager := newTestManagerWithDB(t, db, 1, 4096)
	e := manager.(*viewManager)

	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	commitFinalizeRelease(t, manager)

	select {
	case <-e.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("manager context was not cancelled after the flush failure")
	}

	closeErr := make(chan error, 1)
	go func() { closeErr <- manager.Close() }()
	select {
	case err := <-closeErr:
		require.ErrorContains(t, err, "disk full", "Close must surface the latched fatal error")
	case <-time.After(2 * time.Second):
		t.Fatal("Close hung after the brick")
	}
}

func TestBackpressureWaiterUnblocksOnBrick(t *testing.T) {
	db := newTestDB(nil)
	db.commitBlock = make(chan struct{})

	cfg := newTestConfig(1, 4096)
	cfg.MaxUnflushedVersions = 1
	manager := newTestManagerWithConfig(t, cfg, db)

	// First view starts a flush that stalls in Commit; the second accumulates past the cap.
	commitFinalizeRelease(t, manager)
	commitFinalizeRelease(t, manager)

	blockedErr := make(chan error, 1)
	go func() {
		_, err := manager.Commit()
		blockedErr <- err
	}()

	select {
	case <-blockedErr:
		close(db.commitBlock)
		t.Fatal("View should have blocked on backpressure")
	case <-time.After(50 * time.Millisecond):
	}

	// Fail the stalled flush. The commitErr write happens-before close(commitBlock), which the
	// flusher's receive synchronizes with.
	db.commitErr = errors.New("disk full")
	close(db.commitBlock)

	select {
	case err := <-blockedErr:
		require.ErrorContains(t, err, "disk full",
			"the backpressure waiter must unblock with the underlying flush error")
	case <-time.After(2 * time.Second):
		t.Fatal("backpressure waiter did not unblock after the brick")
	}
}

// Releasing the final reservation without finalizing is a contract violation the manager cannot
// recover from: the view can never be flushed (flush skips unfinalized versions) and so never
// retired, and the caller has spent its Release, so every later version would stall behind it forever
// with its in-memory data accumulating. It must brick rather than return an error and wedge quietly.
func TestFinalReleaseWithoutFinalizeBricks(t *testing.T) {
	db := newTestDB(map[string][]byte{"k": []byte("v")})
	manager := newTestManagerWithDB(t, db, 1, 1<<20)
	e := manager.(*viewManager)

	// Warm the cache, so the refused read below cannot be explained by a DB read.
	v, found, err := manager.Get([]byte("k"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), v)

	view, err := manager.Commit()
	require.NoError(t, err)

	// Release the reservation Commit handed us, without ever finalizing.
	require.ErrorContains(t, view.Release(), "without first being finalized")

	// This brick is synchronous (DecrementReferenceCount already holds versionLock), so unlike a
	// read failure there is nothing to wait for.
	require.Error(t, e.ctx.Err(), "the unfinalized release must cancel the manager context")

	_, err = manager.Commit()
	require.ErrorContains(t, err, "without first being finalized", "Commit must report the latched cause")

	_, _, err = manager.Get([]byte("k"), true)
	require.ErrorContains(t, err, "without first being finalized", "reads must stop once the manager bricks")

	_, err = manager.BatchGet([][]byte{[]byte("k")})
	require.ErrorContains(t, err, "without first being finalized", "batch reads must stop too")

	require.ErrorContains(t, manager.Close(), "without first being finalized")
}

// The counterpart to the above: a reference-count call naming a bogus version leaves manager state
// untouched, so it must report a plain error and leave the manager usable — that caller can retry
// with the right version.
func TestBadVersionReferenceCountErrorsDoNotBrick(t *testing.T) {
	db := newTestDB(map[string][]byte{"k": []byte("v")})
	manager := newTestManagerWithDB(t, db, 1, 1<<20)
	e := manager.(*viewManager)

	require.Error(t, e.IncrementReferenceCount(9999))
	require.Error(t, e.DecrementReferenceCount(9999))
	require.NoError(t, e.ctx.Err(), "a bogus version must not brick the manager")

	// Still fully usable: reads serve, and a view completes its whole lifecycle.
	v, found, err := manager.Get([]byte("k"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), v)
	commitFinalizeRelease(t, manager)
}

// Close must tear down everything the manager owns, even when the caller's context stays live:
// shard contexts (which gate in-flight read waits) and the metrics scrape loop.

func TestCloseCancelsShardContexts(t *testing.T) {
	db := newTestDB(nil)
	manager := newTestManagerWithDB(t, db, 2, 4096)
	e := manager.(*viewManager)

	require.NoError(t, manager.Close())
	for i, s := range e.shards {
		require.Error(t, s.cache.ctx.Err(), "shard %d context must be cancelled by Close", i)
	}
}

func TestMetricsCollectLoopStopsOnCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var scrapes atomic.Int64
	newViewManagerMetrics(ctx, "test-scrape", time.Millisecond,
		func() (uint64, uint64) {
			scrapes.Add(1)
			return 0, 0
		})

	require.Eventually(t, func() bool { return scrapes.Load() > 0 },
		2*time.Second, time.Millisecond, "scrape loop never ran")
	cancel()

	// The loop may finish at most one scrape that raced the cancellation; wait for the count to
	// stabilize, then confirm it stays put.
	var lastSeen int64
	require.Eventually(t, func() bool {
		now := scrapes.Load()
		stable := now == lastSeen
		lastSeen = now
		return stable
	}, 2*time.Second, 20*time.Millisecond, "scrape count did not stabilize after cancellation")

	final := scrapes.Load()
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, final, scrapes.Load(), "scrape loop kept running after ctx cancellation")
}
