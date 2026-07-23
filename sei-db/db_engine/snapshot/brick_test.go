package snapshot

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A flush failure must brick the engine cleanly: no panic, blocking methods unblock, and methods
// that observe the shutdown report an error wrapping the underlying cause.

func TestFlushFailureBricksEngineCleanly(t *testing.T) {
	db := newTestDB(nil)
	db.commitErr = errors.New("disk full")
	engine := newTestEngineWithDB(t, db, 1, 4096)
	e := engine.(*snapshotEngine)

	engine.Set([]byte("k"), []byte("v"))
	snap1, err := engine.Snapshot()
	require.NoError(t, err)

	// A second snapshot whose hash never arrives; its AwaitHash waiter must be released by the
	// brick rather than hang.
	snap2, err := engine.Snapshot()
	require.NoError(t, err)
	awaitHashErr := make(chan error, 1)
	go func() {
		_, hashErr := snap2.AwaitHash(context.Background())
		awaitHashErr <- hashErr
	}()

	// Make snap1 flush-eligible; the flush attempt fails and bricks the engine.
	require.NoError(t, snap1.SetHash(testHash))
	require.NoError(t, snap1.Release())

	select {
	case <-e.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("engine context was not cancelled after the flush failure")
	}

	_, err = engine.Snapshot()
	require.ErrorContains(t, err, "disk full", "Snapshot must report the underlying flush error")

	err = snap1.AwaitFlush(context.Background())
	require.ErrorContains(t, err, "disk full", "AwaitFlush must report the underlying flush error")

	select {
	case hashErr := <-awaitHashErr:
		require.Error(t, hashErr, "AwaitHash must fail once the engine has shut down")
	case <-time.After(2 * time.Second):
		t.Fatal("AwaitHash waiter did not unblock after the brick")
	}
}

func TestCloseAfterBrickReportsFatalError(t *testing.T) {
	db := newTestDB(nil)
	db.commitErr = errors.New("disk full")
	engine := newTestEngineWithDB(t, db, 1, 4096)
	e := engine.(*snapshotEngine)

	engine.Set([]byte("k"), []byte("v"))
	snapshotAndHashRelease(t, engine)

	select {
	case <-e.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("engine context was not cancelled after the flush failure")
	}

	closeErr := make(chan error, 1)
	go func() { closeErr <- engine.Close() }()
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
	engine := newTestEngineWithConfig(t, cfg, db)

	// First snapshot starts a flush that stalls in Commit; the second accumulates past the cap.
	snapshotAndHashRelease(t, engine)
	snapshotAndHashRelease(t, engine)

	blockedErr := make(chan error, 1)
	go func() {
		_, err := engine.Snapshot()
		blockedErr <- err
	}()

	select {
	case <-blockedErr:
		close(db.commitBlock)
		t.Fatal("Snapshot should have blocked on backpressure")
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

// Close must tear down everything the engine owns, even when the caller's context stays live:
// shard contexts (which gate in-flight read waits) and the metrics scrape loop.

func TestCloseCancelsShardContexts(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 2, 4096)
	e := engine.(*snapshotEngine)

	require.NoError(t, engine.Close())
	for i, s := range e.shards {
		require.Error(t, s.ctx.Err(), "shard %d context must be cancelled by Close", i)
	}
}

func TestMetricsCollectLoopStopsOnCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var scrapes atomic.Int64
	newSnapshotEngineMetrics(ctx, "test-scrape", time.Millisecond,
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
