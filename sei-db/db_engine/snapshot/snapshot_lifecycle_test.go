package snapshot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSnapshotSetHashThenReleaseHappyPath(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash))
	require.NoError(t, snap.Release())
}

func TestSnapshotSetHashNilFails(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Commit()
	require.NoError(t, err)
	require.Error(t, snap.SetHash(nil))
	hashAndRelease(t, snap) // clean up with a valid hash
}

func TestSnapshotSetHashTwiceFails(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash))
	require.Error(t, snap.SetHash(testHash))
	require.NoError(t, snap.Release())
}

func TestSnapshotReleaseWithoutHashIsFatal(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Commit()
	require.NoError(t, err)
	require.Error(t, snap.Release(), "releasing the final reservation on an unhashed snapshot must fail")
}

func TestSnapshotDoubleReleaseFails(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash))
	require.NoError(t, snap.Release())
	require.Error(t, snap.Release())
}

func TestSnapshotReserveExtendsLifetime(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	snap, err := engine.Commit()
	require.NoError(t, err)

	require.NoError(t, snap.Reserve()) // refCount = 2
	require.NoError(t, snap.SetHash(testHash))
	require.NoError(t, snap.Release()) // refCount = 1, still alive

	val, found, err := snap.Get([]byte("k"), false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), val)

	require.NoError(t, snap.Release()) // refCount = 0
}

func TestSnapshotAwaitHashReturnsImmediatelyIfSet(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash))

	got, err := snap.AwaitHash(context.Background())
	require.NoError(t, err)
	require.Equal(t, testHash, got)
	require.NoError(t, snap.Release())
}

func TestSnapshotAwaitHashBlocksUntilSet(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Commit()
	require.NoError(t, err)

	got := make(chan []byte, 1)
	go func() {
		h, e := snap.AwaitHash(context.Background())
		require.NoError(t, e)
		got <- h
	}()

	select {
	case <-got:
		t.Fatal("AwaitHash returned before SetHash")
	case <-time.After(30 * time.Millisecond):
	}

	require.NoError(t, snap.SetHash(testHash))
	select {
	case h := <-got:
		require.Equal(t, testHash, h)
	case <-time.After(time.Second):
		t.Fatal("AwaitHash did not unblock after SetHash")
	}
	require.NoError(t, snap.Release())
}

func TestSnapshotAwaitHashContextCancelled(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Commit()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = snap.AwaitHash(ctx)
	require.Error(t, err)

	hashAndRelease(t, snap)
}

func TestSnapshotSetHashGatesFlush(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	db := engine.(*snapshotEngine).db.(*testDB)

	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	snap, err := engine.Commit()
	require.NoError(t, err)

	// Without SetHash, nothing may flush.
	require.Never(t, func() bool { return db.has("k") }, 40*time.Millisecond, 5*time.Millisecond,
		"unhashed snapshot must not flush")

	require.NoError(t, snap.SetHash(testHash))
	awaitFlushed(t, snap, time.Second)
	require.NoError(t, snap.Release())

	val, ok := db.get("k")
	require.True(t, ok)
	require.Equal(t, []byte("v"), val)
}

func TestSnapshotAwaitFlushContextCancelled(t *testing.T) {
	db := newTestDB(nil)
	db.commitBlock = make(chan struct{})
	defer close(db.commitBlock)

	engine := newTestEngineWithDB(t, db, 1, 4096)
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	snap, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	require.Error(t, snap.AwaitFlush(ctx), "flush is stalled, so AwaitFlush must time out")

	require.NoError(t, snap.Release())
}

func TestAwaitFlushRetiredVersionWithCancelledCtx(t *testing.T) {
	engine, _ := newTestEngine(t, nil, 1, 1<<20)
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	snap, err := engine.Commit()
	require.NoError(t, err)
	ver := snap.(*snapshotImpl).version
	hashAndRelease(t, snap)
	awaitRetired(t, engine, ver)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, snap.AwaitFlush(ctx),
		"a retired (untracked) version reports an error even though its flush completed")
}

func TestBackpressureBlocksAndUnblocksOnFlush(t *testing.T) {
	db := newTestDB(nil)
	db.commitBlock = make(chan struct{})

	cfg := newTestConfig(1, 4096)
	cfg.MaxUnflushedVersions = 2
	engine := newTestEngineWithConfig(t, cfg, db)

	// Accumulate more unflushed-but-eligible versions than MaxUnflushedVersions (flush is stalled).
	for i := 0; i < 3; i++ {
		commitAndHashRelease(t, engine)
	}

	blocked := make(chan struct{})
	go func() {
		snap, err := engine.Commit()
		require.NoError(t, err)
		hashAndRelease(t, snap)
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
// behind a still-referenced snapshot are not flushable, so they must not count toward
// MaxUnflushedVersions. Regression test: the flush-eligibility scan used to count them, blocking
// Commit indefinitely while the underlying DB sat idle.
func TestHeldReservationDoesNotTriggerCommitBackpressure(t *testing.T) {
	db := newTestDB(nil)
	cfg := newTestConfig(1, 4096)
	cfg.MaxUnflushedVersions = 2
	engine := newTestEngineWithConfig(t, cfg, db)

	// v1 is hashed and flushed but never released: it cannot retire, so no later version can
	// flush until it is released.
	require.NoError(t, engine.Set([]byte("k1"), []byte("v1")))
	snap1, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap1.SetHash(testHash))
	awaitFlushed(t, snap1, 2*time.Second)

	// Accumulate well more than MaxUnflushedVersions hashed-and-released versions behind the
	// held snapshot. None of them are flushable, so none may count toward backpressure and no
	// Commit may block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ {
			commitAndHashRelease(t, engine)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Commit blocked on backpressure while flushing was stalled on a held reservation")
	}

	// Releasing the blocker unblocks the pipeline: everything behind it flushes and retires.
	require.NoError(t, snap1.Release())
	awaitRetired(t, engine, 6)
	require.True(t, db.has("k1"))
}

func TestCloseLeavesInjectedResourcesOpen(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 4096)
	require.NoError(t, engine.Close())
	require.False(t, db.isClosed(),
		"the DB is injected and caller-owned; the engine must not close it")
}

func TestCloseDoesNotFlush(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 4096)

	// v1 is never hashed, which deterministically keeps the background flusher away from v2:
	// the flush frontier stops at the first unhashed version.
	require.NoError(t, engine.Set([]byte("k1"), []byte("v1")))
	_, err := engine.Commit()
	require.NoError(t, err)

	require.NoError(t, engine.Set([]byte("k2"), []byte("v2")))
	snap2, err := engine.Commit()
	require.NoError(t, err)
	hashAndRelease(t, snap2)

	// Close abandons everything unflushed — hashed or not. Recovery is the upstream WAL's job.
	require.NoError(t, engine.Close())
	require.False(t, db.has("k1"), "Close must not flush unhashed snapshots")
	require.False(t, db.has("k2"), "Close must not flush hashed snapshots either")
}

func TestCloseIsIdempotent(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 4096)
	require.NoError(t, engine.Close())
	require.NoError(t, engine.Close(), "a second Close must be a safe no-op")
}

func TestCloseSkipsUnhashedSnapshot(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 4096)
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	_, err := engine.Commit()
	require.NoError(t, err)
	// Never hashed.
	require.NoError(t, engine.Close())
	require.False(t, db.has("k"), "Close must not flush unhashed snapshots")
}
