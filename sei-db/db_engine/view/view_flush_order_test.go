package view

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// opKeys names the keys of a recorded batch, in the order the flush appended them.
func opKeys(ops []testBatchOp) []string {
	keys := make([]string, 0, len(ops))
	for _, op := range ops {
		keys = append(keys, string(op.key))
	}
	return keys
}

// A version's writes must reach the batch ordered by key: that ascending order is the whole point of
// sorting the diff ahead of the flush, and map iteration order would destroy it.
func TestFlushWritesEachVersionsKeysInAscendingOrder(t *testing.T) {
	manager, db := newTestManager(t, nil, 4, 1<<20)

	// Deliberately neither sorted nor reverse sorted, and spread across shards so the sort has to
	// merge them rather than inherit one shard's order.
	written := []string{"mango", "cherry", "zucchini", "apple", "quince", "banana", "fig"}
	for _, key := range written {
		require.NoError(t, manager.Set([]byte(key), []byte("v")))
	}

	view, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, view.Finalize(hashWrites(testHash)))
	awaitFlushed(t, view, time.Second)
	require.NoError(t, view.Release())

	batches := db.committedBatches()
	require.Len(t, batches, 1, "one version at this size must flush in a single batch")
	keys := opKeys(batches[0])

	// The finalization write is appended after the version's own data, even though its reserved
	// prefix sorts ahead of every data key.
	require.Equal(t, testHashKey, keys[len(keys)-1], "finalization writes must come last")

	dataKeys := keys[:len(keys)-1]
	require.Equal(t, len(written), len(dataKeys))
	require.True(t, slices.IsSorted(dataKeys), "a version's keys must reach the batch ascending, got %v",
		dataKeys)
}

// Sorting is per version and never across the versions sharing a flush: pebble resolves two writes to
// one key by the sequence number it assigns in batch order, so an older version's writes have to reach
// the batch before a newer version's even when they sort later.
func TestFlushOrdersWithinEachVersionNotAcrossThem(t *testing.T) {
	manager, db := newTestManager(t, nil, 1, 1<<20)

	require.NoError(t, manager.Set([]byte("shared"), []byte("from-v1")))
	require.NoError(t, manager.Set([]byte("zebra"), []byte("from-v1")))
	view1, err := manager.Commit()
	require.NoError(t, err)

	require.NoError(t, manager.Set([]byte("aardvark"), []byte("from-v2")))
	require.NoError(t, manager.Set([]byte("shared"), []byte("from-v2")))
	view2, err := manager.Commit()
	require.NoError(t, err)

	// The newer version is finalized and released first so that nothing is flush eligible until the
	// oldest is finalized, which then covers both versions (see
	// TestTargetBytesPerFlushSplitsIntoMultipleCommits).
	finalizeAndRelease(t, view2)
	finalizeAndRelease(t, view1)
	awaitRetired(t, manager, 2)

	// Flattened across batches: where the flush split them does not change the order the writes were
	// appended in, and that order is what pebble numbers.
	var keys []string
	for _, batch := range db.committedBatches() {
		keys = append(keys, opKeys(batch)...)
	}
	require.Equal(t, []string{"shared", "zebra", testHashKey, "aardvark", "shared", testHashKey}, keys,
		"each version's own keys ascend, and the older version's writes all precede the newer's")

	value, ok := db.get("shared")
	require.True(t, ok)
	require.Equal(t, "from-v2", string(value), "the newer version's write must win")
}

// A version's encoded diff is a batch the sort pool built and nobody consumed unless that version
// flushes. Close has to release the ones left over, before the database they came from goes away.
func TestCloseReleasesTheDiffBatchesTheFlushNeverTook(t *testing.T) {
	db := newTestDB(nil)
	manager := newTestManagerWithConfig(t, newTestConfig(1, 4096), db)

	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	_, err := manager.Commit() // seals version 1, and deliberately leaves it unfinalized
	require.NoError(t, err)

	// An unfinalized version never becomes flushable, so its batch sits in the channel. Waiting for
	// the delivery is what makes this deterministic: a batch still being built when Close runs is
	// left to the garbage collector by design, and would not be counted here.
	awaitSortedDiffDelivery(t, manager, 1)

	require.NoError(t, manager.Close())

	require.Positive(t, db.batchesCreated.Load(), "the sort pool must have built a batch")
	require.Equal(t, db.batchesCreated.Load(), db.batchesClosed.Load(),
		"every batch the sort pool built must be closed, including the unflushed one")
}

// awaitSortedDiffDelivery blocks until the sort pool has delivered a version's encoded diff, without
// consuming it.
func awaitSortedDiffDelivery(t *testing.T, manager ViewManager, version uint64) {
	t.Helper()
	m := manager.(*viewManager)
	require.Eventually(t, func() bool {
		m.versionLock.Lock()
		defer m.versionLock.Unlock()
		counter, tracked := m.versionMap[version]
		return tracked && len(counter.sortedDiff) == 1
	}, 2*time.Second, 2*time.Millisecond, "the sorted diff at version %d was not delivered in time",
		version)
}
