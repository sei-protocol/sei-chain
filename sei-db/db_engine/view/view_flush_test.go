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
	view, err := manager.Commit(manager.NextBlockHeight())
	require.NoError(t, err)
	require.NoError(t, view.Finalize(view.BlockHeight(), hashWrites([]byte("the-hash"))))
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

// Finalize takes a whole write set, not a single hash: every pair must land, and each block's
// pairs must land with that block rather than being collapsed across a multi-block flush.
func TestFlushPersistsEveryFinalizationPairPerVersion(t *testing.T) {
	manager, db := newTestManager(t, nil, 1, 1<<20)

	writes := func(blockHeight string) []*proto.KVPair {
		return []*proto.KVPair{
			{Key: []byte("_meta/hash"), Value: []byte("hash-" + blockHeight)},
			{Key: []byte("_meta/block"), Value: []byte(blockHeight)},
			{Key: []byte("_meta/x:evm/stats"), Value: []byte("stats-" + blockHeight)},
		}
	}

	require.NoError(t, manager.Set([]byte("k"), []byte("v1")))
	view1, err := manager.Commit(manager.NextBlockHeight())
	require.NoError(t, err)
	require.NoError(t, view1.Finalize(view1.BlockHeight(), writes("1")))
	require.NoError(t, view1.Release())

	require.NoError(t, manager.Set([]byte("k"), []byte("v2")))
	view2, err := manager.Commit(manager.NextBlockHeight())
	require.NoError(t, err)
	require.NoError(t, view2.Finalize(view2.BlockHeight(), writes("2")))
	awaitFlushed(t, view2, time.Second)
	require.NoError(t, view2.Release())

	// The newer block's metadata wins, and no pair is dropped.
	for key, want := range map[string]string{
		"_meta/hash":        "hash-2",
		"_meta/block":       "2",
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
	view1, err := manager.Commit(manager.NextBlockHeight())
	require.NoError(t, err)
	finalizeAwaitFlushAndRelease(t, view1)

	require.NoError(t, manager.Set([]byte("k"), []byte("v2")))
	view2, err := manager.Commit(manager.NextBlockHeight())
	require.NoError(t, err)
	finalizeAwaitFlushAndRelease(t, view2)

	kv, ok := db.get("k")
	require.True(t, ok)
	require.Equal(t, []byte("v2"), kv, "the later flushed block's value must win in the DB")
}

func TestFlushRacesAheadOfRelease(t *testing.T) {
	manager, db := newTestManager(t, nil, 1, 1<<20)
	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit(manager.NextBlockHeight())
	require.NoError(t, err)

	// Finalize but do NOT release: a finalized oldest view may flush with a reservation outstanding.
	require.NoError(t, view.Finalize(view.BlockHeight(), hashWrites(testHash)))
	awaitFlushed(t, view, time.Second)
	require.True(t, db.has("k"), "finalized view must flush even with an outstanding reservation")

	require.NoError(t, view.Release())
}

func TestFlushBlockedByUnfinalizedEarlierView(t *testing.T) {
	manager, db := newTestManager(t, nil, 1, 1<<20)

	require.NoError(t, manager.Set([]byte("a"), []byte("1")))
	view1, err := manager.Commit(manager.NextBlockHeight())
	require.NoError(t, err)

	require.NoError(t, manager.Set([]byte("b"), []byte("2")))
	view2, err := manager.Commit(manager.NextBlockHeight())
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
	view1, err := manager.Commit(manager.NextBlockHeight()) // block 1
	require.NoError(t, err)
	require.NoError(t, manager.Set([]byte("b"), []byte("2")))
	view2, err := manager.Commit(manager.NextBlockHeight()) // block 2
	require.NoError(t, err)

	// view1 is finalized but held, and view2 is released out of order (before view1).
	require.NoError(t, view1.Finalize(view1.BlockHeight(), hashWrites(testHash)))
	finalizeAndRelease(t, view2)

	// view2 cannot retire while view1 is still held.
	require.Never(t, func() bool { return !isTracked(manager, 2) }, 50*time.Millisecond, 5*time.Millisecond,
		"newer block must not retire while an older block is still held")

	require.NoError(t, view1.Release())
	awaitRetired(t, manager, 2)
}

func TestTargetBytesPerFlushSplitsIntoMultipleCommits(t *testing.T) {
	db := newTestDB(nil)
	cfg := newTestConfig(1, 1<<20)
	// Each block contributes two 2-byte-key/1-byte-value writes plus the finalization entry,
	// roughly 34 encoded bytes (see testBatch.Len); 64 forces a split every couple of blocks.
	cfg.TargetBytesPerFlush = 64
	cfg.MaxUnflushedVersions = 64
	manager := newTestManagerWithConfig(t, cfg, db)

	const versions = 5
	views := make([]View, versions)
	for i := 0; i < versions; i++ {
		require.NoError(t, manager.Set([]byte{byte('a' + i), '1'}, []byte("v")))
		require.NoError(t, manager.Set([]byte{byte('a' + i), '2'}, []byte("v")))
		s, err := manager.Commit(manager.NextBlockHeight())
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
	awaitRetired(t, manager, versions) // last block retired => everything flushed

	require.Greater(t, db.commitCount.Load(), int64(1),
		"a multi-block flush should split into multiple commits at the TargetBytesPerFlush boundary")
}

// Every batch the flusher creates must be closed after commit: the types.Batch contract requires
// Close even on success (pebble batches leak memory otherwise). Regression test: the flusher used
// to commit batches without ever closing them.
func TestFlushClosesEveryBatch(t *testing.T) {
	db := newTestDB(nil)
	cfg := newTestConfig(1, 1<<20)
	cfg.TargetBytesPerFlush = 64 // force multiple batches across the flushed blocks
	cfg.MaxUnflushedVersions = 64
	manager := newTestManagerWithConfig(t, cfg, db)

	const versions = 5
	for i := 0; i < versions; i++ {
		require.NoError(t, manager.Set([]byte{byte('a' + i)}, []byte("v")))
		commitFinalizeRelease(t, manager)
	}
	awaitRetired(t, manager, versions) // last block retired => everything flushed

	require.Greater(t, db.batchesCreated.Load(), int64(1), "expected the flush to span multiple batches")
	require.Equal(t, db.batchesCreated.Load(), db.batchesClosed.Load(),
		"every created batch must be closed exactly once")
}

func TestReserveAfterRetirementFails(t *testing.T) {
	manager, _ := newTestManager(t, nil, 1, 1<<20)
	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit(manager.NextBlockHeight())
	require.NoError(t, err)
	ver := view.(*viewImpl).blockHeight
	finalizeAndRelease(t, view)
	awaitRetired(t, manager, ver)

	require.Error(t, view.Reserve(), "reserving a retired view must fail")
}

func TestFinalizeAfterRetirementFails(t *testing.T) {
	manager, _ := newTestManager(t, nil, 1, 1<<20)
	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit(manager.NextBlockHeight())
	require.NoError(t, err)
	ver := view.(*viewImpl).blockHeight
	finalizeAndRelease(t, view)
	awaitRetired(t, manager, ver)

	require.Error(t, view.Finalize(view.BlockHeight(), hashWrites(testHash)), "finalizing a retired view must fail")
}

func TestAwaitFlushAfterRetirementFails(t *testing.T) {
	manager, _ := newTestManager(t, nil, 1, 1<<20)
	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit(manager.NextBlockHeight())
	require.NoError(t, err)
	ver := view.(*viewImpl).blockHeight
	finalizeAndRelease(t, view)
	awaitRetired(t, manager, ver)

	require.Error(t, view.AwaitFlush(context.Background()), "AwaitFlush on a retired view must fail")
}
