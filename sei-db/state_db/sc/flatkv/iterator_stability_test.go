package flatkv

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// TestIteratorKeyValueStability pins the public iterator contract that
// sei-cosmos callers assume (and memiavl/IAVL provide): Key() and Value()
// slices remain intact after the iterator advances. Staking's EndBlock queue
// processing does store.Delete(iter.Key()) mid-iteration and cachekv keys its
// dirty-entry maps by zero-copy strings of those slices; if the iterator
// serves Pebble's reused buffers, the retained keys mutate as iteration
// advances and the deletes are silently dropped from the block's changeset.
// That is exactly the forked-chain wedge: the unbonding-queue deletions
// vanished (differently on each node) and block replay panicked on the
// ghost entries.
func TestIteratorKeyValueStability(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	const n = 2000
	expected := make(map[string]string, n)
	pairs := make([]*proto.KVPair, 0, n)
	for i := 0; i < n; i++ {
		key := make([]byte, 1+8)
		key[0] = 0x43 // staking ValidatorQueueKey prefix, for flavor
		binary.BigEndian.PutUint64(key[1:], uint64(i))
		value := []byte(fmt.Sprintf("value-%06d-%s", i, string(make([]byte, 64))))
		expected[string(key)] = string(value)
		pairs = append(pairs, &proto.KVPair{Key: key, Value: value})
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "staking", Changeset: proto.ChangeSet{Pairs: pairs}},
	}))
	commitAndCheck(t, s)

	// Retain every Key()/Value() slice across Next(), like staking's
	// delete-while-iterating loops and cachekv's unsafe key strings do.
	iter, err := s.Iterator("staking", []byte{0x43}, []byte{0x44}, true)
	require.NoError(t, err)
	var retainedKeys, retainedValues [][]byte
	for ; iter.Valid(); iter.Next() {
		retainedKeys = append(retainedKeys, iter.Key())
		retainedValues = append(retainedValues, iter.Value())
	}
	require.NoError(t, iter.Error())
	require.NoError(t, iter.Close())
	require.Len(t, retainedKeys, n)

	// Every retained slice must still hold the entry it was read as.
	for i, key := range retainedKeys {
		want, ok := expected[string(key)]
		require.True(t, ok, "retained key %d mutated after Next: %x", i, key)
		require.Equal(t, want, string(retainedValues[i]),
			"retained value %d mutated after Next", i)
		delete(expected, string(key))
	}
	require.Empty(t, expected, "retained keys must cover every written entry exactly once")

	// End-to-end: deleting via the retained keys must remove every entry —
	// this is the exact production shape whose corrupted keys made the
	// unbonding-queue deletions no-ops.
	deletePairs := make([]*proto.KVPair, 0, n)
	for _, key := range retainedKeys {
		deletePairs = append(deletePairs, &proto.KVPair{Key: key, Delete: true})
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "staking", Changeset: proto.ChangeSet{Pairs: deletePairs}},
	}))
	commitAndCheck(t, s)

	iter2, err := s.Iterator("staking", []byte{0x43}, []byte{0x44}, true)
	require.NoError(t, err)
	defer iter2.Close()
	for ; iter2.Valid(); iter2.Next() {
		t.Fatalf("ghost entry survived deletion: %x", iter2.Key())
	}
	require.NoError(t, iter2.Error())
}
