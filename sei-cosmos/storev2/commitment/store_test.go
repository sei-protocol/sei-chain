package commitment

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
)

func TestLastCommitID(t *testing.T) {
	tree := memiavl.New(100)
	store := NewStore(tree, log.NewNopLogger())
	require.Equal(t, types.CommitID{Hash: tree.RootHash()}, store.LastCommitID())
}

func TestGetReadsFromChangeSet(t *testing.T) {
	tree := memiavl.New(100)
	store := NewStore(tree, log.NewNopLogger())

	// Initially key doesn't exist
	require.Nil(t, store.Get([]byte("key")))
	require.False(t, store.Has([]byte("key")))

	// Set without commit - should be visible via Get/Has
	store.Set([]byte("key"), []byte("value"))
	require.Equal(t, []byte("value"), store.Get([]byte("key")))
	require.True(t, store.Has([]byte("key")))

	// Update the same key - should see the latest value
	store.Set([]byte("key"), []byte("updated"))
	require.Equal(t, []byte("updated"), store.Get([]byte("key")))
	require.True(t, store.Has([]byte("key")))

	// Delete without commit - should appear deleted
	store.Delete([]byte("key"))
	require.Nil(t, store.Get([]byte("key")))
	require.False(t, store.Has([]byte("key")))

	// Set again after delete - should be visible
	store.Set([]byte("key"), []byte("resurrected"))
	require.Equal(t, []byte("resurrected"), store.Get([]byte("key")))
	require.True(t, store.Has([]byte("key")))
}

func TestGetReadsFromChangeSetMultipleKeys(t *testing.T) {
	tree := memiavl.New(100)
	store := NewStore(tree, log.NewNopLogger())

	// Set multiple keys
	store.Set([]byte("key1"), []byte("value1"))
	store.Set([]byte("key2"), []byte("value2"))
	store.Set([]byte("key3"), []byte("value3"))

	// All should be visible
	require.Equal(t, []byte("value1"), store.Get([]byte("key1")))
	require.Equal(t, []byte("value2"), store.Get([]byte("key2")))
	require.Equal(t, []byte("value3"), store.Get([]byte("key3")))

	// Delete one key
	store.Delete([]byte("key2"))
	require.Equal(t, []byte("value1"), store.Get([]byte("key1")))
	require.Nil(t, store.Get([]byte("key2")))
	require.Equal(t, []byte("value3"), store.Get([]byte("key3")))
}

func TestPopChangeSetClearsBuffer(t *testing.T) {
	tree := memiavl.New(100)
	store := NewStore(tree, log.NewNopLogger())

	// Set a key
	store.Set([]byte("key"), []byte("value"))
	require.Equal(t, []byte("value"), store.Get([]byte("key")))

	// Pop the changeset
	cs := store.PopChangeSet()
	require.Len(t, cs.Pairs, 1)

	// After pop, key should not be visible (not in changeSet anymore, not in tree)
	require.Nil(t, store.Get([]byte("key")))
	require.False(t, store.Has([]byte("key")))
}
