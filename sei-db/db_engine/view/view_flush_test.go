package view

import (
	"context"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestFlushPersistsSetsDeletesAndFinalizationToDB(t *testing.T) {
	manager, db := newTestManager(t, map[string][]byte{"del": []byte("x")}, 1, 1<<20)

	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	require.NoError(t, manager.Delete([]byte("del")))
	view, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, view.Finalize(hashWrites([]byte("the-hash"))))
	awaitFlushed(t, view, time.Second)
	require.NoError(t, view.Release())

	kv, ok := db.get("k")
	require.True(t, ok)
	require.Equal(t, []byte("v"), kv)
	require.False(t, db.has("del"), "delete must be flushed as a DB delete")
	h, ok := db.get(testHashKey)
	require.True(t, ok, "finalization writes must be persisted")
	require.Equal(t, []byte("the-hash"), h)
}

// Finalize takes a whole write set, not a single hash: every pair must land, and each version's
// pairs must land with that version rather than being collapsed across a multi-version flush.
func TestFlushPersistsEveryFinalizationPairPerVersion(t *testing.T) {
	manager, db := newTestManager(t, nil, 1, 1<<20)

	writes := func(version string) []*proto.KVPair {
		return []*proto.KVPair{
			{Key: []byte("_meta/hash"), Value: []byte("hash-" + version)},
			{Key: []byte("_meta/version"), Value: []byte(version)},
			{Key: []byte("_meta/x:evm/stats"), Value: []byte("stats-" + version)},
		}
	}

	require.NoError(t, manager.Set([]byte("k"), []byte("v1")))
	view1, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, view1.Finalize(writes("1")))
	require.NoError(t, view1.Release())

	require.NoError(t, manager.Set([]byte("k"), []byte("v2")))
	view2, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, view2.Finalize(writes("2")))
	awaitFlushed(t, view2, time.Second)
	require.NoError(t, view2.Release())

	// The newer version's metadata wins, and no pair is dropped.
	for key, want := range map[string]string{
		"_meta/hash":        "hash-2",
		"_meta/version":     "2",
		"_meta/x:evm/stats": "stats-2",
	} {
		got, ok := db.get(key)
		require.True(t, ok, "finalization key %q must be persisted", key)
		require.Equal(t, want, string(got), "finalization key %q", key)
	}
}

func TestFlushLatestValueWinsAcrossVersions(t *testing.T) {
	manager, db := newTestManager(t, nil, 1, 1<<20)

	// Finalize, wait, then release. AwaitFlush requires the reservation to be held across the call:
	// a released view can be retired out from under it, and the wait is then undefined.
	require.NoError(t, manager.Set([]byte("k"), []byte("v1")))
	view1, err := manager.Commit()
	require.NoError(t, err)
	finalizeAwaitFlushAndRelease(t, view1)

	require.NoError(t, manager.Set([]byte("k"), []byte("v2")))
	view2, err := manager.Commit()
	require.NoError(t, err)
	finalizeAwaitFlushAndRelease(t, view2)

	kv, ok := db.get("k")
	require.True(t, ok)
	require.Equal(t, []byte("v2"), kv, "the later flushed version's value must win in the DB")
}

func TestFlushRacesAheadOfRelease(t *testing.T) {
	manager, db := newTestManager(t, nil, 1, 1<<20)
	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit()
	require.NoError(t, err)

	// Finalize but do NOT release: a finalized oldest view may flush with a reservation outstanding.
	require.NoError(t, view.Finalize(hashWrites(testHash)))
	awaitFlushed(t, view, time.Second)
	require.True(t, db.has("k"), "finalized view must flush even with an outstanding reservation")

	require.NoError(t, view.Release())
}

func TestFlushBlockedByUnfinalizedEarlierView(t *testing.T) {
	manager, db := newTestManager(t, nil, 1, 1<<20)

	require.NoError(t, manager.Set([]byte("a"), []byte("1")))
	view1, err := manager.Commit()
	require.NoError(t, err)

	require.NoError(t, manager.Set([]byte("b"), []byte("2")))
	view2, err := manager.Commit()
	require.NoError(t, err)

	// Finalize+release view2 first: it must NOT flush while view1 is unfinalized.
	finalizeAndRelease(t, view2)
	require.Never(t, func() bool { return db.has("a") || db.has("b") }, 50*time.Millisecond, 5*time.Millisecond,
		"nothing may flush while the oldest view is unfinalized")

	// Finalize+release view1: both flush, in order.
	finalizeAndRelease(t, view1)
	require.Eventually(t, func() bool { return db.has("a") && db.has("b") }, time.Second, 5*time.Millisecond)
}

func TestOutOfOrderReleaseDoesNotRetireNewer(t *testing.T) {
	manager, _ := newTestManager(t, nil, 1, 1<<20)

	require.NoError(t, manager.Set([]byte("a"), []byte("1")))
	view1, err := manager.Commit() // version 1
	require.NoError(t, err)
	require.NoError(t, manager.Set([]byte("b"), []byte("2")))
	view2, err := manager.Commit() // version 2
	require.NoError(t, err)

	require.NoError(t, view1.Finalize(hashWrites(testHash))) // finalized but held
	finalizeAndRelease(t, view2)                             // released out of order (before view1)

	// view2 cannot retire while view1 is still held.
	require.Never(t, func() bool { return !isTracked(manager, 2) }, 50*time.Millisecond, 5*time.Millisecond,
		"newer version must not retire while an older held version blocks it")

	require.NoError(t, view1.Release())
	awaitRetired(t, manager, 2)
}

func TestTargetBytesPerFlushSplitsIntoMultipleCommits(t *testing.T) {
	db := newTestDB(nil)
	cfg := newTestConfig(1, 1<<20)
	// Each version contributes two 2-byte-key/1-byte-value writes plus the finalization entry,
	// roughly 34 encoded bytes (see testBatch.Len); 64 forces a split every couple of versions.
	cfg.TargetBytesPerFlush = 64
	cfg.MaxUnflushedVersions = 64
	manager := newTestManagerWithConfig(t, cfg, db)

	const versions = 5
	views := make([]View, versions)
	for i := 0; i < versions; i++ {
		require.NoError(t, manager.Set([]byte{byte('a' + i), '1'}, []byte("v")))
		require.NoError(t, manager.Set([]byte{byte('a' + i), '2'}, []byte("v")))
		s, err := manager.Commit()
		require.NoError(t, err)
		views[i] = s
	}

	// Finalize+release all but the oldest, so nothing is flush-eligible yet (eligibility breaks at the
	// unfinalized oldest). This makes the eventual flush cover the whole contiguous prefix.
	for i := 1; i < versions; i++ {
		finalizeAndRelease(t, views[i])
	}
	require.Equal(t, int64(0), db.commitCount.Load(), "nothing should flush while the oldest is unfinalized")

	finalizeAndRelease(t, views[0])
	awaitRetired(t, manager, versions) // last version retired => everything flushed

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
	manager := newTestManagerWithConfig(t, cfg, db)

	const versions = 5
	for i := 0; i < versions; i++ {
		require.NoError(t, manager.Set([]byte{byte('a' + i)}, []byte("v")))
		commitFinalizeRelease(t, manager)
	}
	awaitRetired(t, manager, versions) // last version retired => everything flushed

	require.Greater(t, db.batchesCreated.Load(), int64(1), "expected the flush to span multiple batches")
	require.Equal(t, db.batchesCreated.Load(), db.batchesClosed.Load(),
		"every created batch must be closed exactly once")
}

func TestReserveAfterRetirementFails(t *testing.T) {
	manager, _ := newTestManager(t, nil, 1, 1<<20)
	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit()
	require.NoError(t, err)
	ver := view.(*viewImpl).version
	finalizeAndRelease(t, view)
	awaitRetired(t, manager, ver)

	require.Error(t, view.Reserve(), "reserving a retired view must fail")
}

func TestFinalizeAfterRetirementFails(t *testing.T) {
	manager, _ := newTestManager(t, nil, 1, 1<<20)
	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit()
	require.NoError(t, err)
	ver := view.(*viewImpl).version
	finalizeAndRelease(t, view)
	awaitRetired(t, manager, ver)

	require.Error(t, view.Finalize(hashWrites(testHash)), "finalizing a retired view must fail")
}

func TestAwaitFlushAfterRetirementFails(t *testing.T) {
	manager, _ := newTestManager(t, nil, 1, 1<<20)
	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit()
	require.NoError(t, err)
	ver := view.(*viewImpl).version
	finalizeAndRelease(t, view)
	awaitRetired(t, manager, ver)

	require.Error(t, view.AwaitFlush(context.Background()), "AwaitFlush on a retired view must fail")
}
