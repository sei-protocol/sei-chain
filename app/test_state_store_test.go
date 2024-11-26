package app

import (
	"testing"

	"github.com/cosmos/iavl"
	seidbproto "github.com/sei-protocol/sei-db/proto"
	"github.com/stretchr/testify/assert"
)

func TestApplyChangesetAndGet(t *testing.T) {
	store := NewInMemoryStateStore()

	err := store.ApplyChangeset(1, &seidbproto.NamedChangeSet{
		Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: []byte("key1"), Value: []byte("value1")},
			},
		},
		Name: "exampleStore",
	})
	assert.NoError(t, err)

	value, err := store.Get("exampleStore", 1, []byte("key1"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("value1"), value)
}

func TestHas(t *testing.T) {
	store := NewInMemoryStateStore()

	err := store.ApplyChangeset(1, &seidbproto.NamedChangeSet{
		Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: []byte("key1"), Value: []byte("value1")},
			},
		},
		Name: "exampleStore",
	})
	assert.NoError(t, err)

	has, err := store.Has("exampleStore", 1, []byte("key1"))
	assert.NoError(t, err)
	assert.True(t, has)

	has, err = store.Has("exampleStore", 1, []byte("key2"))
	assert.NoError(t, err)
	assert.False(t, has)
}

func TestIterator(t *testing.T) {
	store := NewInMemoryStateStore()

	err := store.ApplyChangeset(1, &seidbproto.NamedChangeSet{
		Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: []byte("key1"), Value: []byte("value1")},
				{Key: []byte("key2"), Value: []byte("value2")},
			},
		},
		Name: "exampleStore",
	})
	assert.NoError(t, err)

	iter, err := store.Iterator("exampleStore", 1, nil, nil)
	assert.NoError(t, err)
	assert.NotNil(t, iter)

	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("key1"), iter.Key())
	assert.Equal(t, []byte("value1"), iter.Value())

	iter.Next()
	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("key2"), iter.Key())
	assert.Equal(t, []byte("value2"), iter.Value())

	iter.Next()
	assert.False(t, iter.Valid())
	iter.Close()
}

func TestReverseIterator(t *testing.T) {
	store := NewInMemoryStateStore()

	err := store.ApplyChangeset(1, &seidbproto.NamedChangeSet{
		Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: []byte("key1"), Value: []byte("value1")},
				{Key: []byte("key2"), Value: []byte("value2")},
			},
		},
		Name: "exampleStore",
	})
	assert.NoError(t, err)

	iter, err := store.ReverseIterator("exampleStore", 1, nil, nil)
	assert.NoError(t, err)
	assert.NotNil(t, iter)

	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("key2"), iter.Key())
	assert.Equal(t, []byte("value2"), iter.Value())

	iter.Next()
	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("key1"), iter.Key())
	assert.Equal(t, []byte("value1"), iter.Value())

	iter.Next()
	assert.False(t, iter.Valid())
	iter.Close()
}

func TestGetLatestVersionAndSetLatestVersion(t *testing.T) {
	store := NewInMemoryStateStore()

	err := store.SetLatestVersion(2)
	assert.NoError(t, err)

	version, err := store.GetLatestVersion()
	assert.NoError(t, err)
	assert.Equal(t, int64(2), version)
}

func TestGetEarliestVersionAndSetEarliestVersion(t *testing.T) {
	store := NewInMemoryStateStore()

	err := store.SetEarliestVersion(1, false)
	assert.NoError(t, err)

	version, err := store.GetEarliestVersion()
	assert.NoError(t, err)
	assert.Equal(t, int64(1), version)
}

func TestPrune(t *testing.T) {
	store := NewInMemoryStateStore()

	err := store.ApplyChangeset(1, &seidbproto.NamedChangeSet{
		Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: []byte("key1"), Value: []byte("value1")},
				{Key: []byte("key2"), Value: []byte("value2")},
			},
		},
		Name: "exampleStore",
	})
	assert.NoError(t, err)

	err = store.Prune(1)
	assert.NoError(t, err)

	_, err = store.Get("exampleStore", 1, []byte("key1"))
	assert.Error(t, err)

	_, err = store.Get("exampleStore", 1, []byte("key2"))
	assert.Error(t, err)
}

func TestClose(t *testing.T) {
	store := NewInMemoryStateStore()

	err := store.Close()
	assert.NoError(t, err)
}
