package view

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// A key reaches the manager as bytes through Get, Set, Delete and BatchGet, and as a string through
// BatchSet and bucketIndicesByShard. Both must route it to the same shard, or a key written by one
// path is invisible to the other.
func TestShardAndShardStringAgree(t *testing.T) {
	manager, err := newShardManager(8)
	require.NoError(t, err)

	keys := [][]byte{
		nil,
		{},
		[]byte("a"),
		[]byte("evm/"),
		make([]byte, 57),
	}
	for i := 0; i < 256; i++ {
		keys = append(keys, []byte(fmt.Sprintf("evm/%d/%s", i, string(rune('a'+i%26)))))
	}

	for _, key := range keys {
		require.Equal(t, manager.Shard(key), manager.ShardString(string(key)),
			"key %q must reach the same shard through either form", key)
	}
}

func TestShardIsWithinRange(t *testing.T) {
	for _, shardCount := range []uint64{1, 2, 8, 64} {
		manager, err := newShardManager(shardCount)
		require.NoError(t, err)

		for i := 0; i < 1000; i++ {
			key := []byte(fmt.Sprintf("key%d", i))
			require.Less(t, manager.Shard(key), shardCount)
			require.Less(t, manager.ShardString(string(key)), shardCount)
		}
	}
}

func TestShardCountMustBeAPowerOfTwo(t *testing.T) {
	for _, shardCount := range []uint64{0, 3, 5, 6, 7, 9, 100} {
		_, err := newShardManager(shardCount)
		require.ErrorIs(t, err, ErrNumShardsNotPowerOfTwo, "shard count %d", shardCount)
	}
}

// Bucketing splits the hashing across the pool, so a key's position must still land in the bucket for
// its own shard, every position must survive, and positions within a bucket must stay ascending — a
// batch that writes one key twice relies on that order to apply the writes in the order given.
func TestBucketIndicesByShardPreservesOrderAndMembership(t *testing.T) {
	for _, count := range []int{0, 1, 2, 7, 8, 9, 100, 4096} {
		manager := newTestManagerWithDB(t, newTestDB(nil), 8, 1<<20)
		impl := manager.(*viewManager)

		keys := make([]string, count)
		for i := range keys {
			keys[i] = fmt.Sprintf("evm/%d", i)
		}

		buckets := impl.bucketIndicesByShard(count, func(i int) string { return keys[i] })
		require.Len(t, buckets, 8, "count %d", count)

		seen := make([]bool, count)
		for shardIndex, bucket := range buckets {
			for n, position := range bucket {
				require.Equal(t, uint64(shardIndex), impl.shardManager.ShardString(keys[position]),
					"count %d: position %d is in the wrong shard's bucket", count, position)
				require.False(t, seen[position], "count %d: position %d bucketed twice", count, position)
				seen[position] = true
				if n > 0 {
					require.Greater(t, position, bucket[n-1],
						"count %d: shard %d lost ascending order", count, shardIndex)
				}
			}
		}
		for position, ok := range seen {
			require.True(t, ok, "count %d: position %d was dropped", count, position)
		}
	}
}
