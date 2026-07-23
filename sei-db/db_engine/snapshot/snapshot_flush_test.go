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

	engine.Set([]byte("k"), []byte("v"))
	engine.Delete([]byte("del"))
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

	engine.Set([]byte("k"), []byte("v1"))
	snap1, err := engine.Commit()
	require.NoError(t, err)
	hashAndRelease(t, snap1)
	awaitFlushed(t, snap1, time.Second)

	engine.Set([]byte("k"), []byte("v2"))
	snap2, err := engine.Commit()
	require.NoError(t, err)
	hashAndRelease(t, snap2)
	awaitFlushed(t, snap2, time.Second)

	kv, ok := db.get("k")
	require.True(t, ok)
	require.Equal(t, []byte("v2"), kv, "the later flushed version's value must win in the DB")
}

func TestFlushRacesAheadOfRelease(t *testing.T) {
	engine, db := newTestEngine(t, nil, 1, 1<<20)
	engine.Set([]byte("k"), []byte("v"))
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

	engine.Set([]byte("a"), []byte("1"))
	snap1, err := engine.Commit()
	require.NoError(t, err)

	engine.Set([]byte("b"), []byte("2"))
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

	engine.Set([]byte("a"), []byte("1"))
	snap1, err := engine.Commit() // version 1
	require.NoError(t, err)
	engine.Set([]byte("b"), []byte("2"))
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

func TestTargetKeysPerFlushSplitsIntoMultipleCommits(t *testing.T) {
	db := newTestDB(nil)
	cfg := newTestConfig(1, 1<<20)
	cfg.TargetKeysPerFlush = 3
	cfg.MaxUnflushedVersions = 64
	engine := newTestEngineWithConfig(t, cfg, db)

	const versions = 5
	snaps := make([]Snapshot, versions)
	for i := 0; i < versions; i++ {
		engine.Set([]byte{byte('a' + i), '1'}, []byte("v"))
		engine.Set([]byte{byte('a' + i), '2'}, []byte("v"))
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
		"a multi-version flush should split into multiple commits at the TargetKeysPerFlush boundary")
}

func TestReserveAfterRetirementFails(t *testing.T) {
	engine, _ := newTestEngine(t, nil, 1, 1<<20)
	engine.Set([]byte("k"), []byte("v"))
	snap, err := engine.Commit()
	require.NoError(t, err)
	ver := snap.(*snapshotImpl).version
	hashAndRelease(t, snap)
	awaitRetired(t, engine, ver)

	require.Error(t, snap.Reserve(), "reserving a retired snapshot must fail")
}

func TestAwaitHashAfterRetirementFails(t *testing.T) {
	engine, _ := newTestEngine(t, nil, 1, 1<<20)
	engine.Set([]byte("k"), []byte("v"))
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
	engine.Set([]byte("k"), []byte("v"))
	snap, err := engine.Commit()
	require.NoError(t, err)
	ver := snap.(*snapshotImpl).version
	hashAndRelease(t, snap)
	awaitRetired(t, engine, ver)

	require.Error(t, snap.AwaitFlush(context.Background()), "AwaitFlush on a retired snapshot must fail")
}
