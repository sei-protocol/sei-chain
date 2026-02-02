package composite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/stretchr/testify/require"
)

func setupTestStores(t *testing.T) (*CompositeStateStore, string, func()) {
	dir, err := os.MkdirTemp("", "composite_store_test")
	require.NoError(t, err)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0, // Sync writes for tests
		KeepRecent:       100000,
	}

	evmConfig := &config.EVMStateStoreConfig{
		Enable:      true,
		EnableRead:  true,
		EnableWrite: true,
		DBDirectory: filepath.Join(dir, "evm_ss"),
		KeepRecent:  100000,
	}

	compositeStore, err := NewCompositeStateStore(ssConfig, *evmConfig, dir, logger.NewNopLogger())
	require.NoError(t, err)

	cleanup := func() {
		compositeStore.Close()
		os.RemoveAll(dir)
	}

	return compositeStore, dir, cleanup
}

func TestCompositeStateStoreRead(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	t.Run("Get from Cosmos store", func(t *testing.T) {
		// Write via ApplyChangesetSync (goes to Cosmos only in this PR)
		changesets := []*proto.NamedChangeSet{
			{
				Name: "bank",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("balance1"), Value: []byte("100")},
					},
				},
			},
		}
		err := store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

		// Read back
		val, err := store.Get("bank", 1, []byte("balance1"))
		require.NoError(t, err)
		require.Equal(t, []byte("100"), val)

		// Has
		has, err := store.Has("bank", 1, []byte("balance1"))
		require.NoError(t, err)
		require.True(t, has)

		// Non-existent
		val, err = store.Get("bank", 1, []byte("nonexistent"))
		require.NoError(t, err)
		require.Nil(t, val)
	})

	t.Run("Get EVM key falls back to Cosmos", func(t *testing.T) {
		// Write EVM data via Cosmos store (ApplyChangesetSync doesn't dual-write in this PR)
		addr := make([]byte, 20)
		slot := make([]byte, 32)
		storageKey := append([]byte{0x03}, append(addr, slot...)...) // StateKeyPrefix

		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: storageKey, Value: []byte("storage_value")},
					},
				},
			},
		}
		err := store.ApplyChangesetSync(2, changesets)
		require.NoError(t, err)

		// Read should fallback to Cosmos store since EVM_SS doesn't have the data yet
		val, err := store.Get("evm", 2, storageKey)
		require.NoError(t, err)
		require.Equal(t, []byte("storage_value"), val)
	})
}

func TestCompositeStateStoreIterator(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	// Write some data
	changesets := []*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("a"), Value: []byte("1")},
					{Key: []byte("b"), Value: []byte("2")},
					{Key: []byte("c"), Value: []byte("3")},
				},
			},
		},
	}
	err := store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	t.Run("Forward iteration", func(t *testing.T) {
		iter, err := store.Iterator("test", 1, nil, nil)
		require.NoError(t, err)
		defer iter.Close()

		var keys []string
		for ; iter.Valid(); iter.Next() {
			keys = append(keys, string(iter.Key()))
		}
		require.Equal(t, []string{"a", "b", "c"}, keys)
	})

	t.Run("Reverse iteration", func(t *testing.T) {
		iter, err := store.ReverseIterator("test", 1, nil, nil)
		require.NoError(t, err)
		defer iter.Close()

		var keys []string
		for ; iter.Valid(); iter.Next() {
			keys = append(keys, string(iter.Key()))
		}
		require.Equal(t, []string{"c", "b", "a"}, keys)
	})
}

func TestCompositeStateStoreVersions(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	// Initially no version
	require.Equal(t, int64(0), store.GetLatestVersion())

	// Write at version 1
	changesets := []*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key"), Value: []byte("v1")},
				},
			},
		},
	}
	err := store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	require.Equal(t, int64(1), store.GetLatestVersion())
}

func TestCompositeStateStoreWithoutEVM(t *testing.T) {
	dir, err := os.MkdirTemp("", "composite_no_evm_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
	}

	// Create composite store with EVM disabled (Enable=false)
	evmConfig := config.EVMStateStoreConfig{Enable: false}
	store, err := NewCompositeStateStore(ssConfig, evmConfig, dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	// Should work fine without EVM
	changesets := []*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key"), Value: []byte("value")},
				},
			},
		},
	}
	err = store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	val, err := store.Get("test", 1, []byte("key"))
	require.NoError(t, err)
	require.Equal(t, []byte("value"), val)
}

func TestCompositeStateStoreHas(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	// Write data
	changesets := []*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("exists"), Value: []byte("value")},
				},
			},
		},
	}
	err := store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	// Has existing key
	has, err := store.Has("test", 1, []byte("exists"))
	require.NoError(t, err)
	require.True(t, has)

	// Has non-existing key
	has, err = store.Has("test", 1, []byte("nonexistent"))
	require.NoError(t, err)
	require.False(t, has)

	// Has at wrong version
	has, err = store.Has("test", 0, []byte("exists"))
	require.NoError(t, err)
	require.False(t, has)
}
