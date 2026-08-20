package snapshot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFlushPersistsSetsDeletesAndHashToDB(t *testing.T) {
	engine, db := newTestEngine(t, map[string][]byte{"del": []byte("x")}, 1, 1<<20)
	hashKey := engine.(*snapshotEngine).config.HashKey

	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	require.NoError(t, engine.Delete([]byte("del")))
	snap, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash([]byte("the-hash")))
	awaitFlushed(t, snap, time.Second)
	require.NoError(t, snap.Release())

	kv, ok := db.get("k")
	require.True(t, ok)
	require.Equal(t, []byte("v"), kv)
	require.False(t, db.has("del"), "delete must be flushed as a DB delete")
	h, ok := db.get(hashKey)
	require.True(t, ok, "hash must be persisted under the hash key")
	require.Equal(t, []byte("the-hash"), h)
}

func TestFlushLatestValueWinsAcrossVersions(t *testing.T) {
	engine, db := newTestEngine(t, nil, 1, 1<<20)

	require.NoError(t, engine.Set([]byte("k"), []byte("v1")))
	snap1, err := engine.Commit()
	require.NoError(t, err)
	hashAwaitFlushAndRelease(t, snap1)

	require.NoError(t, engine.Set([]byte("k"), []byte("v2")))
	snap2, err := engine.Commit()
	require.NoError(t, err)
	hashAwaitFlushAndRelease(t, snap2)

	kv, ok := db.get("k")
	require.True(t, ok)
	require.Equal(t, []byte("v2"), kv, "the later flushed version's value must win in the DB")
}

func TestFlushRacesAheadOfRelease(t *testing.T) {
	engine, db := newTestEngine(t, nil, 1, 1<<20)
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	snap, err := engine.Commit()
	require.NoError(t, err)

	// Hash but do NOT release: a hashed oldest snapshot may flush while a reservation is outstanding.
	require.NoError(t, snap.SetHash(testHash))
	awaitFlushed(t, snap, time.Second)
	require.True(t, db.has("k"), "hashed snapshot must flush even with an outstanding reservation")

	require.NoError(t, snap.Release())
}

func TestFlushBlockedByUnhashedEarlierSnapshot(t *testing.T) {
	engine, db := newTestEngine(t, nil, 1, 1<<20)

	require.NoError(t, engine.Set([]byte("a"), []byte("1")))
	snap1, err := engine.Commit()
	require.NoError(t, err)

	require.NoError(t, engine.Set([]byte("b"), []byte("2")))
	snap2, err := engine.Commit()
	require.NoError(t, err)

	// Hash+release snap2 first: it must NOT flush while snap1 is unhashed.
	hashAndRelease(t, snap2)
	require.Never(t, func() bool { return db.has("a") || db.has("b") }, 50*time.Millisecond, 5*time.Millisecond,
		"nothing may flush while the oldest snapshot is unhashed")

	// Hash+release snap1: both flush, in order.
	hashAndRelease(t, snap1)
	require.Eventually(t, func() bool { return db.has("a") && db.has("b") }, time.Second, 5*time.Millisecond)
}

func TestOutOfOrderReleaseDoesNotRetireNewer(t *testing.T) {
	engine, _ := newTestEngine(t, nil, 1, 1<<20)

	require.NoError(t, engine.Set([]byte("a"), []byte("1")))
	snap1, err := engine.Commit() // version 1
	require.NoError(t, err)
	require.NoError(t, engine.Set([]byte("b"), []byte("2")))
	snap2, err := engine.Commit() // version 2
	require.NoError(t, err)

	require.NoError(t, snap1.SetHash(testHash)) // hashed but held
	hashAndRelease(t, snap2)                    // released out of order (before snap1)

	// snap2 cannot retire while snap1 is still held.
	require.Never(t, func() bool { return !isTracked(engine, 2) }, 50*time.Millisecond, 5*time.Millisecond,
		"newer version must not retire while an older held version blocks it")

	require.NoError(t, snap1.Release())
	awaitRetired(t, engine, 2)
}

func TestTargetBytesPerFlushSplitsIntoMultipleCommits(t *testing.T) {
	db := newTestDB(nil)
	cfg := newTestConfig(1, 1<<20)
	// Each version contributes two 2-byte-key/1-byte-value writes plus the hash-key entry,
	// roughly 34 encoded bytes (see testBatch.Len); 64 forces a split every couple of versions.
	cfg.TargetBytesPerFlush = 64
	cfg.MaxUnflushedVersions = 64
	engine := newTestEngineWithConfig(t, cfg, db)

	const versions = 5
	snaps := make([]Snapshot, versions)
	for i := 0; i < versions; i++ {
		require.NoError(t, engine.Set([]byte{byte('a' + i), '1'}, []byte("v")))
		require.NoError(t, engine.Set([]byte{byte('a' + i), '2'}, []byte("v")))
		s, err := engine.Commit()
		require.NoError(t, err)
		snaps[i] = s
	}

	// Hash+release all but the oldest, so nothing is flush-eligible yet (eligibility breaks at the
	// unhashed oldest). This makes the eventual flush cover the whole contiguous prefix.
	for i := 1; i < versions; i++ {
		hashAndRelease(t, snaps[i])
	}
	require.Equal(t, int64(0), db.commitCount.Load(), "nothing should flush while the oldest is unhashed")

	hashAndRelease(t, snaps[0])
	awaitRetired(t, engine, versions) // last version retired => everything flushed

	require.Greater(t, db.commitCount.Load(), int64(1),
		"a multi-version flush should split into multiple commits at the TargetBytesPerFlush boundary")
}

// Every batch the flusher creates must be closed after commit: the types.Batch contract requires
// Close even on success (pebble batches leak memory otherwise). Regression test: the flusher used
// to commit batches without ever closing them.
func TestFlushClosesEveryBatch(t *testing.T) {
	db := newTestDB(nil)
	cfg := newTestConfig(1, 1<<20)
	cfg.TargetBytesPerFlush = 64 // force multiple batches across the flushed versions
	cfg.MaxUnflushedVersions = 64
	engine := newTestEngineWithConfig(t, cfg, db)

	const versions = 5
	for i := 0; i < versions; i++ {
		require.NoError(t, engine.Set([]byte{byte('a' + i)}, []byte("v")))
		commitAndHashRelease(t, engine)
	}
	awaitRetired(t, engine, versions) // last version retired => everything flushed

	require.Greater(t, db.batchesCreated.Load(), int64(1), "expected the flush to span multiple batches")
	require.Equal(t, db.batchesCreated.Load(), db.batchesClosed.Load(),
		"every created batch must be closed exactly once")
}

func TestReserveAfterRetirementFails(t *testing.T) {
	engine, _ := newTestEngine(t, nil, 1, 1<<20)
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	snap, err := engine.Commit()
	require.NoError(t, err)
	ver := snap.(*snapshotImpl).version
	hashAndRelease(t, snap)
	awaitRetired(t, engine, ver)

	require.Error(t, snap.Reserve(), "reserving a retired snapshot must fail")
}

func TestAwaitHashAfterRetirementFails(t *testing.T) {
	engine, _ := newTestEngine(t, nil, 1, 1<<20)
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	snap, err := engine.Commit()
	require.NoError(t, err)
	ver := snap.(*snapshotImpl).version
	hashAndRelease(t, snap)
	awaitRetired(t, engine, ver)

	_, err = snap.AwaitHash(context.Background())
	require.Error(t, err, "AwaitHash on a retired snapshot must fail")
}

func TestAwaitFlushAfterRetirementFails(t *testing.T) {
	engine, _ := newTestEngine(t, nil, 1, 1<<20)
	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	snap, err := engine.Commit()
	require.NoError(t, err)
	ver := snap.(*snapshotImpl).version
	hashAndRelease(t, snap)
	awaitRetired(t, engine, ver)

	require.Error(t, snap.AwaitFlush(context.Background()), "AwaitFlush on a retired snapshot must fail")
}
