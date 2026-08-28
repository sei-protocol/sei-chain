package view

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// Shutdown contract under test: when Close returns, no manager-owned goroutine will touch the
// injected DB or pools again; no caller is left deadlocked in a method; blocked callers may resolve
// with either a real value or an error. Close does not flush and does not touch the injected DB or
// pools.

// Close must wait for the lifecycle runner to report offline, even when the runner is stalled
// inside a batch commit.
func TestCloseWaitsForLifecycleMidCommit(t *testing.T) {
	db := newTestDB(nil)
	manager := newTestManagerWithDB(t, db, 1, 4096)

	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit()
	require.NoError(t, err)

	db.commitBlock = make(chan struct{})
	require.NoError(t, view.Finalize(hashWrites(testHash)))

	// Wait until the flusher is stalled inside Commit.
	require.Eventually(t, func() bool { return db.commitEntered.Load() > 0 },
		2*time.Second, time.Millisecond, "flusher never reached Commit")

	closeDone := make(chan error, 1)
	go func() { closeDone <- manager.Close() }()

	select {
	case <-closeDone:
		close(db.commitBlock)
		t.Fatal("Close returned while the lifecycle runner was still inside a commit")
	case <-time.After(100 * time.Millisecond):
	}

	close(db.commitBlock)
	select {
	case err := <-closeDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after the stalled commit was released")
	}
}

// Close must release AwaitFlush waiters with errors wrapping ErrViewManagerClosed, both for a view
// that can never flush on its own account and for one stuck behind it.
func TestCloseUnblocksFlushWaiters(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 1, 4096)

	// v1 is never finalized, so it can never flush; the flush frontier stops there, which strands v2
	// as well even though v2 is finalized.
	unfinalized, err := manager.Commit()
	require.NoError(t, err)
	finalized, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, finalized.Finalize(hashWrites(testHash)))

	unfinalizedErr := make(chan error, 1)
	strandedErr := make(chan error, 1)
	go func() {
		unfinalizedErr <- unfinalized.AwaitFlush(context.Background())
	}()
	go func() {
		strandedErr <- finalized.AwaitFlush(context.Background())
	}()

	// Let the waiters park; neither may return while the manager is healthy.
	select {
	case err := <-unfinalizedErr:
		t.Fatalf("unfinalized AwaitFlush returned before Close: %v", err)
	case err := <-strandedErr:
		t.Fatalf("stranded AwaitFlush returned before Close: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	require.NoError(t, manager.Close())

	for name, ch := range map[string]chan error{"unfinalized": unfinalizedErr, "stranded": strandedErr} {
		select {
		case err := <-ch:
			require.ErrorIs(t, err, ErrViewManagerClosed, "%s AwaitFlush must report ErrViewManagerClosed", name)
		case <-time.After(2 * time.Second):
			t.Fatalf("%s AwaitFlush waiter did not unblock after Close", name)
		}
	}
}

// A Commit() call blocked on lifecycle backpressure must not outlive Close. Depending on how
// the drain races the cancellation it may resolve with a view or with an error; either is
// acceptable — it just must not deadlock.
func TestCloseUnblocksBackpressuredCommit(t *testing.T) {
	db := newTestDB(nil)
	db.commitBlock = make(chan struct{})

	cfg := newTestConfig(1, 4096)
	cfg.MaxUnflushedVersions = 1
	manager := newTestManagerWithConfig(t, cfg, db)

	// The first view's flush stalls in Commit; the second accumulates past the cap, so the
	// next Commit() blocks on backpressure.
	commitFinalizeRelease(t, manager)
	commitFinalizeRelease(t, manager)

	blockedDone := make(chan error, 1)
	go func() {
		_, err := manager.Commit()
		blockedDone <- err
	}()
	select {
	case err := <-blockedDone:
		close(db.commitBlock)
		t.Fatalf("View returned before Close: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- manager.Close() }()

	// Close waits for the stalled runner; release it so teardown can complete.
	close(db.commitBlock)

	select {
	case <-blockedDone:
		// Value or error — both fine; the waiter just must not be deadlocked.
	case <-time.After(2 * time.Second):
		t.Fatal("backpressured View did not unblock after Close")
	}
	select {
	case err := <-closeDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after the stalled commit was released")
	}
}

// A read whose DB fetch is still in flight when Close is called must resolve (value or error)
// rather than deadlock. The manager must not wait for caller-owned pool tasks.
func TestBlockedReadResolvesDuringClose(t *testing.T) {
	db := newTestDB(map[string][]byte{"k": []byte("v")})
	manager := newTestManagerWithDB(t, db, 1, 4096)

	// Baseline past the construction-time initial-hash read, then gate all further DB reads.
	base := db.getCalls.Load()
	db.getGate = make(chan struct{})

	// Release the gated read task exactly once, and unconditionally on test failure, before the
	// pool-draining cleanup registered at construction (t.Cleanup runs LIFO).
	releaseGate := sync.OnceFunc(func() { close(db.getGate) })
	t.Cleanup(releaseGate)

	getDone := make(chan error, 1)
	go func() {
		_, _, err := manager.Get([]byte("k"), true)
		getDone <- err
	}()
	require.Eventually(t, func() bool { return db.getCalls.Load() > base },
		2*time.Second, time.Millisecond, "read task never reached the DB")

	require.NoError(t, manager.Close())

	select {
	case err := <-getDone:
		// The gate is still held, so the read cannot have produced a value: Close must have
		// released the waiter with an error wrapping ErrViewManagerClosed, per the Close contract.
		require.ErrorIs(t, err, ErrViewManagerClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Get did not resolve after Close")
	}

	releaseGate()
}

// Methods called after a clean Close must report ErrViewManagerClosed.
func TestMethodsAfterCloseReportManagerClosed(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 1, 4096)
	view, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, manager.Close())

	_, err = manager.Commit()
	require.ErrorIs(t, err, ErrViewManagerClosed)

	err = view.AwaitFlush(context.Background())
	require.ErrorIs(t, err, ErrViewManagerClosed)

	// Writes must be refused rather than accepted into data that no lifecycle runner remains to
	// flush, and reads must not keep serving from a closed manager.
	require.ErrorIs(t, manager.Set([]byte("k"), []byte("v")), ErrViewManagerClosed)
	require.ErrorIs(t, manager.Delete([]byte("k")), ErrViewManagerClosed)
	require.ErrorIs(t, manager.BatchSet([]*proto.KVPair{{Key: []byte("k"), Value: []byte("v")}}), ErrViewManagerClosed)

	_, _, err = manager.Get([]byte("k"), true)
	require.ErrorIs(t, err, ErrViewManagerClosed)

	_, err = manager.BatchGet([][]byte{[]byte("k")})
	require.ErrorIs(t, err, ErrViewManagerClosed)
}

// Every goroutine the manager owns (lifecycle runner, metrics scrape loop) must be gone once
// Close returns: repeated create/use/close cycles may not grow the process goroutine count.
func TestCloseLeavesNoManagerGoroutines(t *testing.T) {
	cycle := func() {
		cfg := newTestConfig(2, 4096)
		cfg.MetricsEnabled = true
		cfg.MetricsScrapeIntervalSeconds = 0.001
		db := newTestDB(map[string][]byte{"seeded": []byte("v")})
		pool := threading.NewAdHocPool()
		manager, err := NewViewManager(cfg, db, pool, pool)
		require.NoError(t, err)

		require.NoError(t, manager.Set([]byte("k"), []byte("v")))
		_, _, err = manager.Get([]byte("seeded"), true) // read-through miss
		require.NoError(t, err)
		_, err = manager.BatchGet([][]byte{[]byte("seeded"), []byte("k")})
		require.NoError(t, err)
		view, err := manager.Commit()
		require.NoError(t, err)
		finalizeAndRelease(t, view)

		require.NoError(t, manager.Close())
		pool.Close()
	}

	// Warm up once (lazy runtime/otel state), then measure. The +2 slack absorbs testify's
	// Eventually prober and scheduling noise; a real leak grows by several goroutines per
	// cycle and blows well past it.
	cycle()
	baseline := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		cycle()
	}
	require.Eventually(t, func() bool { return runtime.NumGoroutine() <= baseline+2 },
		2*time.Second, 10*time.Millisecond,
		"manager goroutines leaked across create/use/close cycles")
}
