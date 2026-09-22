package view

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// A key reaches the manager as bytes through Get, Set, Delete and BatchGet, and as a string through
// BatchSet and partitionIndicesByShard. Both must route it to the same shard, or a key written by one
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
