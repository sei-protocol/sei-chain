package snapshot

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

// Shutdown contract under test: when Close returns, no engine-owned goroutine will touch the
// injected DB or pools again; no caller is left deadlocked in a method; blocked callers may resolve
// with either a real value or an error. Close does not flush and does not touch the injected DB or
// pools.

// Close must wait for the lifecycle runner to report offline, even when the runner is stalled
// inside a batch commit.
func TestCloseWaitsForLifecycleMidCommit(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 4096)

	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	snap, err := engine.Commit()
	require.NoError(t, err)

	db.commitBlock = make(chan struct{})
	require.NoError(t, snap.Finalize(hashWrites(testHash)))

	// Wait until the flusher is stalled inside Commit.
	require.Eventually(t, func() bool { return db.commitEntered.Load() > 0 },
		2*time.Second, time.Millisecond, "flusher never reached Commit")

	closeDone := make(chan error, 1)
	go func() { closeDone <- engine.Close() }()

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

// Close must release AwaitFlush waiters with errors wrapping ErrEngineClosed, both for a snapshot
// that can never flush on its own account and for one stuck behind it.
func TestCloseUnblocksFlushWaiters(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)

	// v1 is never finalized, so it can never flush; the flush frontier stops there, which strands v2
	// as well even though v2 is finalized.
	unfinalized, err := engine.Commit()
	require.NoError(t, err)
	finalized, err := engine.Commit()
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

	// Let the waiters park; neither may return while the engine is healthy.
	select {
	case err := <-unfinalizedErr:
		t.Fatalf("unfinalized AwaitFlush returned before Close: %v", err)
	case err := <-strandedErr:
		t.Fatalf("stranded AwaitFlush returned before Close: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	require.NoError(t, engine.Close())

	for name, ch := range map[string]chan error{"unfinalized": unfinalizedErr, "stranded": strandedErr} {
		select {
		case err := <-ch:
			require.ErrorIs(t, err, ErrEngineClosed, "%s AwaitFlush must report ErrEngineClosed", name)
		case <-time.After(2 * time.Second):
			t.Fatalf("%s AwaitFlush waiter did not unblock after Close", name)
		}
	}
}

// A Commit() call blocked on lifecycle backpressure must not outlive Close. Depending on how
// the drain races the cancellation it may resolve with a snapshot or with an error; either is
// acceptable — it just must not deadlock.
func TestCloseUnblocksBackpressuredCommit(t *testing.T) {
	db := newTestDB(nil)
	db.commitBlock = make(chan struct{})

	cfg := newTestConfig(1, 4096)
	cfg.MaxUnflushedVersions = 1
	engine := newTestEngineWithConfig(t, cfg, db)

	// The first snapshot's flush stalls in Commit; the second accumulates past the cap, so the
	// next Commit() blocks on backpressure.
	commitFinalizeRelease(t, engine)
	commitFinalizeRelease(t, engine)

	blockedDone := make(chan error, 1)
	go func() {
		_, err := engine.Commit()
		blockedDone <- err
	}()
	select {
	case err := <-blockedDone:
		close(db.commitBlock)
		t.Fatalf("Snapshot returned before Close: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- engine.Close() }()

	// Close waits for the stalled runner; release it so teardown can complete.
	close(db.commitBlock)

	select {
	case <-blockedDone:
		// Value or error — both fine; the waiter just must not be deadlocked.
	case <-time.After(2 * time.Second):
		t.Fatal("backpressured Snapshot did not unblock after Close")
	}
	select {
	case err := <-closeDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after the stalled commit was released")
	}
}

// A read whose DB fetch is still in flight when Close is called must resolve (value or error)
// rather than deadlock. The engine must not wait for caller-owned pool tasks.
func TestBlockedReadResolvesDuringClose(t *testing.T) {
	db := newTestDB(map[string][]byte{"k": []byte("v")})
	engine := newTestEngineWithDB(t, db, 1, 4096)

	// Baseline past the construction-time initial-hash read, then gate all further DB reads.
	base := db.getCalls.Load()
	db.getGate = make(chan struct{})

	// Release the gated read task exactly once, and unconditionally on test failure, before the
	// pool-draining cleanup registered at construction (t.Cleanup runs LIFO).
	releaseGate := sync.OnceFunc(func() { close(db.getGate) })
	t.Cleanup(releaseGate)

	getDone := make(chan error, 1)
	go func() {
		_, _, err := engine.Get([]byte("k"), true)
		getDone <- err
	}()
	require.Eventually(t, func() bool { return db.getCalls.Load() > base },
		2*time.Second, time.Millisecond, "read task never reached the DB")

	require.NoError(t, engine.Close())

	select {
	case err := <-getDone:
		// The gate is still held, so the read cannot have produced a value: Close must have
		// released the waiter with an error wrapping ErrEngineClosed, per the Close contract.
		require.ErrorIs(t, err, ErrEngineClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Get did not resolve after Close")
	}

	releaseGate()
}

// Methods called after a clean Close must report ErrEngineClosed.
func TestMethodsAfterCloseReportEngineClosed(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, engine.Close())

	_, err = engine.Commit()
	require.ErrorIs(t, err, ErrEngineClosed)

	err = snap.AwaitFlush(context.Background())
	require.ErrorIs(t, err, ErrEngineClosed)

	// Writes must be refused rather than accepted into data that no lifecycle runner remains to
	// flush, and reads must not keep serving from a closed engine.
	require.ErrorIs(t, engine.Set([]byte("k"), []byte("v")), ErrEngineClosed)
	require.ErrorIs(t, engine.Delete([]byte("k")), ErrEngineClosed)
	require.ErrorIs(t, engine.BatchSet([]*proto.KVPair{{Key: []byte("k"), Value: []byte("v")}}), ErrEngineClosed)

	_, _, err = engine.Get([]byte("k"), true)
	require.ErrorIs(t, err, ErrEngineClosed)

	_, err = engine.BatchGet([][]byte{[]byte("k")})
	require.ErrorIs(t, err, ErrEngineClosed)
}

// Every goroutine the engine owns (lifecycle runner, metrics scrape loop) must be gone once
// Close returns: repeated create/use/close cycles may not grow the process goroutine count.
func TestCloseLeavesNoEngineGoroutines(t *testing.T) {
	cycle := func() {
		cfg := newTestConfig(2, 4096)
		cfg.MetricsEnabled = true
		cfg.MetricsScrapeIntervalSeconds = 0.001
		db := newTestDB(map[string][]byte{"seeded": []byte("v")})
		pool := threading.NewAdHocPool()
		engine, err := NewSnapshotEngine(cfg, db, pool, pool, pool)
		require.NoError(t, err)

		require.NoError(t, engine.Set([]byte("k"), []byte("v")))
		_, _, err = engine.Get([]byte("seeded"), true) // read-through miss
		require.NoError(t, err)
		_, err = engine.BatchGet([][]byte{[]byte("seeded"), []byte("k")})
		require.NoError(t, err)
		snap, err := engine.Commit()
		require.NoError(t, err)
		finalizeAndRelease(t, snap)

		require.NoError(t, engine.Close())
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
		"engine goroutines leaked across create/use/close cycles")
}
