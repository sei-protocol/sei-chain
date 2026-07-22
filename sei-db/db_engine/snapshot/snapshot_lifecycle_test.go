package snapshot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSnapshotSetHashThenReleaseHappyPath(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Snapshot()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash))
	require.NoError(t, snap.Release())
}

func TestSnapshotSetHashNilFails(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Snapshot()
	require.NoError(t, err)
	require.Error(t, snap.SetHash(nil))
	hashAndRelease(t, snap) // clean up with a valid hash
}

func TestSnapshotSetHashTwiceFails(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Snapshot()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash))
	require.Error(t, snap.SetHash(testHash))
	require.NoError(t, snap.Release())
}

func TestSnapshotReleaseWithoutHashIsFatal(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Snapshot()
	require.NoError(t, err)
	require.Error(t, snap.Release(), "releasing the final reservation on an unhashed snapshot must fail")
}

func TestSnapshotDoubleReleaseFails(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Snapshot()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash))
	require.NoError(t, snap.Release())
	require.Error(t, snap.Release())
}

func TestSnapshotReserveExtendsLifetime(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	engine.Set([]byte("k"), []byte("v"))
	snap, err := engine.Snapshot()
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
	snap, err := engine.Snapshot()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash))

	got, err := snap.AwaitHash(context.Background())
	require.NoError(t, err)
	require.Equal(t, testHash, got)
	require.NoError(t, snap.Release())
}

func TestSnapshotAwaitHashBlocksUntilSet(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 4096)
	snap, err := engine.Snapshot()
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
	snap, err := engine.Snapshot()
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

	engine.Set([]byte("k"), []byte("v"))
	snap, err := engine.Snapshot()
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
	engine.Set([]byte("k"), []byte("v"))
	snap, err := engine.Snapshot()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	require.Error(t, snap.AwaitFlush(ctx), "flush is stalled, so AwaitFlush must time out")

	require.NoError(t, snap.Release())
}

func TestBackpressureBlocksAndUnblocksOnFlush(t *testing.T) {
	db := newTestDB(nil)
	db.commitBlock = make(chan struct{})

	cfg := newTestConfig(1, 4096)
	cfg.MaxUnretiredVersions = 2
	engine := newTestEngineWithConfig(t, cfg, db)

	// Accumulate more unflushed-but-eligible versions than MaxUnretiredVersions (flush is stalled).
	for i := 0; i < 3; i++ {
		snapshotAndHashRelease(t, engine)
	}

	blocked := make(chan struct{})
	go func() {
		snap, err := engine.Snapshot()
		require.NoError(t, err)
		hashAndRelease(t, snap)
		close(blocked)
	}()

	select {
	case <-blocked:
		close(db.commitBlock)
		t.Fatal("Snapshot() should have blocked on backpressure")
	case <-time.After(50 * time.Millisecond):
	}

	// Let the flusher drain; backpressure must release.
	close(db.commitBlock)
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("Snapshot() did not unblock after flush drained")
	}
}

func TestCloseClosesUnderlyingDB(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 4096)
	require.NoError(t, engine.Close())
	require.True(t, db.isClosed())
}

func TestCloseFlushesHashedSnapshot(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 4096)
	engine.Set([]byte("k"), []byte("v"))
	snap, err := engine.Snapshot()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash)) // hashed but NOT released

	require.NoError(t, engine.Close())
	val, ok := db.get("k")
	require.True(t, ok, "Close must flush hashed (flush-eligible) snapshots")
	require.Equal(t, []byte("v"), val)
}

func TestCloseSkipsUnhashedSnapshot(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 4096)
	engine.Set([]byte("k"), []byte("v"))
	_, err := engine.Snapshot()
	require.NoError(t, err)
	// Never hashed.
	require.NoError(t, engine.Close())
	require.False(t, db.has("k"), "Close must not flush unhashed snapshots")
}
