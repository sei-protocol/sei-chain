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
