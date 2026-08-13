package snapshot

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// Writes pick a shard with ShardString and reads pick one with Shard. If those ever disagreed, a key
// written through the string API would be looked for in a different shard than it landed in, and
// would read as absent.
func TestShardStringPicksSameShardAsShardBytes(t *testing.T) {
	manager, err := newShardManager(8)
	require.NoError(t, err)

	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("evm/%d/some-reasonably-long-physical-key", i)
		require.Equal(t, manager.Shard([]byte(key)), manager.ShardString(key),
			"key %q must hash to the same shard whichever form it arrives in", key)
	}

	// The forms an engine actually sees: an empty key, a single byte, and the two EVM key lengths.
	for _, key := range []string{"", "k", "evm/\x0a01234567890123456789", "evm/\x03" + string(make([]byte, 52))} {
		require.Equal(t, manager.Shard([]byte(key)), manager.ShardString(key),
			"key %q must hash to the same shard whichever form it arrives in", key)
	}
}

// BatchSetString must leave the engine in exactly the state BatchSet would, including for deletes
// and for empty-but-present values, which are distinct from deletes.
func TestBatchSetStringMatchesBatchSet(t *testing.T) {
	seed := map[string][]byte{
		"pre-existing": []byte("old"),
		"to-delete":    []byte("doomed"),
	}

	type update struct {
		key    string
		value  []byte
		delete bool
	}
	updates := []update{
		{key: "alpha", value: []byte("a")},
		{key: "beta", value: []byte("b")},
		{key: "pre-existing", value: []byte("new")},
		{key: "to-delete", delete: true},
		{key: "empty-value", value: []byte{}},
		{key: "alpha", value: []byte("a-overwritten")},
	}

	byteEngine, _ := newTestEngine(t, seed, 8, 1<<20)
	bytePairs := make([]*proto.KVPair, 0, len(updates))
	for _, u := range updates {
		bytePairs = append(bytePairs, &proto.KVPair{Key: []byte(u.key), Value: u.value, Delete: u.delete})
	}
	require.NoError(t, byteEngine.BatchSet(bytePairs))

	stringEngine, _ := newTestEngine(t, seed, 8, 1<<20)
	stringPairs := make([]StringKVPair, 0, len(updates))
	for _, u := range updates {
		stringPairs = append(stringPairs, StringKVPair{Key: u.key, Value: u.value, Delete: u.delete})
	}
	require.NoError(t, stringEngine.BatchSetString(stringPairs))

	for _, key := range []string{"alpha", "beta", "pre-existing", "to-delete", "empty-value", "absent"} {
		wantValue, wantFound, wantErr := byteEngine.Get([]byte(key), true)
		gotValue, gotFound, gotErr := stringEngine.Get([]byte(key), true)
		require.NoError(t, wantErr)
		require.NoError(t, gotErr)
		require.Equal(t, wantFound, gotFound, "presence differs for key %q", key)
		require.Equal(t, wantValue, gotValue, "value differs for key %q", key)
	}
}

// A value written through the string API must be readable through the ordinary byte-keyed read path,
// which is the pairing the engine is actually used with.
func TestBatchSetStringIsReadableByByteKey(t *testing.T) {
	engine, _ := newTestEngine(t, nil, 8, 1<<20)

	pairs := make([]StringKVPair, 0, 256)
	for i := 0; i < 256; i++ {
		pairs = append(pairs, StringKVPair{
			Key:   fmt.Sprintf("evm/key-%d", i),
			Value: []byte(fmt.Sprintf("value-%d", i)),
		})
	}
	require.NoError(t, engine.BatchSetString(pairs))

	for i := 0; i < 256; i++ {
		value, found, err := engine.Get([]byte(fmt.Sprintf("evm/key-%d", i)), true)
		require.NoError(t, err)
		require.True(t, found, "key written through BatchSetString must be found by byte key")
		require.Equal(t, []byte(fmt.Sprintf("value-%d", i)), value)
	}
}
