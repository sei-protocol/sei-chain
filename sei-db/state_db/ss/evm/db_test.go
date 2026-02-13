package evm

import (
	"os"
	"sync"
	"testing"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/cosmos/iavl"
	"github.com/stretchr/testify/require"
)

func TestEVMDatabase(t *testing.T) {
	dir, err := os.MkdirTemp("", "evm_db_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	db, err := OpenDB(dir, StoreStorage)
	require.NoError(t, err)
	defer db.Close()

	t.Run("Basic operations", func(t *testing.T) {
		key := []byte("test_key")
		value := []byte("test_value")
		version := int64(1)

		// Set
		err := db.Set(key, value, version)
		require.NoError(t, err)

		// Get
		got, err := db.Get(key, version)
		require.NoError(t, err)
		require.Equal(t, value, got)

		// Has
		has, err := db.Has(key, version)
		require.NoError(t, err)
		require.True(t, has)

		// Get non-existent key
		got, err = db.Get([]byte("non_existent"), version)
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("Version handling", func(t *testing.T) {
		key := []byte("versioned_key")

		// Write v1
		err := db.Set(key, []byte("v1"), 1)
		require.NoError(t, err)

		// Write v5
		err = db.Set(key, []byte("v5"), 5)
		require.NoError(t, err)

		// Write v10
		err = db.Set(key, []byte("v10"), 10)
		require.NoError(t, err)

		// Query at v1 -> get v1
		got, err := db.Get(key, 1)
		require.NoError(t, err)
		require.Equal(t, []byte("v1"), got)

		// Query at v3 -> get v1 (latest <= 3)
		got, err = db.Get(key, 3)
		require.NoError(t, err)
		require.Equal(t, []byte("v1"), got)

		// Query at v7 -> get v5 (latest <= 7)
		got, err = db.Get(key, 7)
		require.NoError(t, err)
		require.Equal(t, []byte("v5"), got)

		// Query at v10 -> get v10
		got, err = db.Get(key, 10)
		require.NoError(t, err)
		require.Equal(t, []byte("v10"), got)

		// Query at v100 -> get v10 (latest <= 100)
		got, err = db.Get(key, 100)
		require.NoError(t, err)
		require.Equal(t, []byte("v10"), got)
	})

	t.Run("Delete (tombstone)", func(t *testing.T) {
		key := []byte("delete_key")

		// Write at v1
		err := db.Set(key, []byte("value"), 1)
		require.NoError(t, err)

		// Delete at v5
		err = db.Delete(key, 5)
		require.NoError(t, err)

		// Query at v1 -> exists
		got, err := db.Get(key, 1)
		require.NoError(t, err)
		require.Equal(t, []byte("value"), got)

		// Query at v5 -> deleted (nil)
		got, err = db.Get(key, 5)
		require.NoError(t, err)
		require.Nil(t, got)

		// Query at v10 -> still deleted
		got, err = db.Get(key, 10)
		require.NoError(t, err)
		require.Nil(t, got)
	})
}

func TestEVMDatabaseBatch(t *testing.T) {
	dir, err := os.MkdirTemp("", "evm_batch_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	db, err := OpenDB(dir, StoreStorage)
	require.NoError(t, err)
	defer db.Close()

	pairs := []*iavl.KVPair{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key2"), Value: []byte("value2")},
		{Key: []byte("key3"), Value: []byte("value3")},
	}

	err = db.ApplyBatch(pairs, 1)
	require.NoError(t, err)

	// Verify all keys
	for _, pair := range pairs {
		got, err := db.Get(pair.Key, 1)
		require.NoError(t, err)
		require.Equal(t, pair.Value, got)
	}

	// Apply batch with delete
	deletePairs := []*iavl.KVPair{
		{Key: []byte("key1"), Delete: true},
		{Key: []byte("key4"), Value: []byte("value4")},
	}

	err = db.ApplyBatch(deletePairs, 2)
	require.NoError(t, err)

	// key1 should be deleted at v2
	got, err := db.Get([]byte("key1"), 2)
	require.NoError(t, err)
	require.Nil(t, got)

	// key1 should still exist at v1
	got, err = db.Get([]byte("key1"), 1)
	require.NoError(t, err)
	require.Equal(t, []byte("value1"), got)

	// key4 should exist
	got, err = db.Get([]byte("key4"), 2)
	require.NoError(t, err)
	require.Equal(t, []byte("value4"), got)
}

func TestEVMStateStore(t *testing.T) {
	dir, err := os.MkdirTemp("", "evm_state_store_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewEVMStateStore(dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	t.Run("Multiple store types", func(t *testing.T) {
		// Write to different store types
		for _, storeType := range AllEVMStoreTypes() {
			db := store.GetDB(storeType)
			require.NotNil(t, db)

			key := []byte("key_" + StoreTypeName(storeType))
			value := []byte("value_" + StoreTypeName(storeType))

			err := db.Set(key, value, 1)
			require.NoError(t, err)

			got, err := db.Get(key, 1)
			require.NoError(t, err)
			require.Equal(t, value, got)
		}
	})

	t.Run("ApplyChangeset", func(t *testing.T) {
		changes := map[EVMStoreType][]*iavl.KVPair{
			StoreStorage: {
				{Key: []byte("storage_key"), Value: []byte("storage_value")},
			},
			StoreNonce: {
				{Key: []byte("nonce_key"), Value: []byte("nonce_value")},
			},
		}

		err := store.ApplyChangeset(10, changes)
		require.NoError(t, err)

		// Verify storage
		got, err := store.GetDB(StoreStorage).Get([]byte("storage_key"), 10)
		require.NoError(t, err)
		require.Equal(t, []byte("storage_value"), got)

		// Verify nonce
		got, err = store.GetDB(StoreNonce).Get([]byte("nonce_key"), 10)
		require.NoError(t, err)
		require.Equal(t, []byte("nonce_value"), got)
	})
}

func TestEVMStateStoreParallel(t *testing.T) {
	dir, err := os.MkdirTemp("", "evm_parallel_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewEVMStateStore(dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	// Apply changes to multiple store types in parallel
	changes := map[EVMStoreType][]*iavl.KVPair{
		StoreStorage: {
			{Key: []byte("s1"), Value: []byte("v1")},
			{Key: []byte("s2"), Value: []byte("v2")},
		},
		StoreNonce: {
			{Key: []byte("n1"), Value: []byte("v1")},
		},
		StoreCode: {
			{Key: []byte("c1"), Value: []byte("code1")},
		},
	}

	err = store.ApplyChangesetParallel(1, changes)
	require.NoError(t, err)

	// Verify
	got, err := store.GetDB(StoreStorage).Get([]byte("s1"), 1)
	require.NoError(t, err)
	require.Equal(t, []byte("v1"), got)

	got, err = store.GetDB(StoreNonce).Get([]byte("n1"), 1)
	require.NoError(t, err)
	require.Equal(t, []byte("v1"), got)

	got, err = store.GetDB(StoreCode).Get([]byte("c1"), 1)
	require.NoError(t, err)
	require.Equal(t, []byte("code1"), got)
}

func TestEVMDatabaseConcurrent(t *testing.T) {
	dir, err := os.MkdirTemp("", "evm_concurrent_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	db, err := OpenDB(dir, StoreStorage)
	require.NoError(t, err)
	defer db.Close()

	// Concurrent writes
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := []byte{byte(i)}
			value := []byte{byte(i * 2)}
			err := db.Set(key, value, int64(i))
			require.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// Verify all writes
	for i := 0; i < 10; i++ {
		key := []byte{byte(i)}
		got, err := db.Get(key, int64(i))
		require.NoError(t, err)
		require.Equal(t, []byte{byte(i * 2)}, got)
	}
}

func TestEVMDatabaseIterator(t *testing.T) {
	dir, err := os.MkdirTemp("", "evm_iterator_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	db, err := OpenDB(dir, StoreStorage)
	require.NoError(t, err)
	defer db.Close()

	// Write keys at version 1
	keys := [][]byte{
		[]byte("a"),
		[]byte("b"),
		[]byte("c"),
		[]byte("d"),
	}
	for _, key := range keys {
		err := db.Set(key, append([]byte("value_"), key...), 1)
		require.NoError(t, err)
	}

	t.Run("Forward iteration", func(t *testing.T) {
		iter, err := db.Iterator(nil, nil, 1)
		require.NoError(t, err)
		defer iter.Close()

		var found []string
		for ; iter.Valid(); iter.Next() {
			found = append(found, string(iter.Key()))
		}
		require.Equal(t, []string{"a", "b", "c", "d"}, found)
	})

	t.Run("Forward iteration with bounds", func(t *testing.T) {
		iter, err := db.Iterator([]byte("b"), []byte("d"), 1)
		require.NoError(t, err)
		defer iter.Close()

		var found []string
		for ; iter.Valid(); iter.Next() {
			found = append(found, string(iter.Key()))
		}
		require.Equal(t, []string{"b", "c"}, found)
	})

	t.Run("Iteration respects version", func(t *testing.T) {
		// Add key "e" at version 5
		err := db.Set([]byte("e"), []byte("value_e"), 5)
		require.NoError(t, err)

		// Iterate at version 1 - should not see "e"
		iter, err := db.Iterator(nil, nil, 1)
		require.NoError(t, err)
		defer iter.Close()

		var found []string
		for ; iter.Valid(); iter.Next() {
			found = append(found, string(iter.Key()))
		}
		require.Equal(t, []string{"a", "b", "c", "d"}, found)

		// Iterate at version 5 - should see "e"
		iter2, err := db.Iterator(nil, nil, 5)
		require.NoError(t, err)
		defer iter2.Close()

		found = nil
		for ; iter2.Valid(); iter2.Next() {
			found = append(found, string(iter2.Key()))
		}
		require.Equal(t, []string{"a", "b", "c", "d", "e"}, found)
	})

	t.Run("Iteration skips tombstones", func(t *testing.T) {
		// Delete "b" at version 10
		err := db.Delete([]byte("b"), 10)
		require.NoError(t, err)

		// Iterate at version 10 - should not see "b"
		iter, err := db.Iterator(nil, nil, 10)
		require.NoError(t, err)
		defer iter.Close()

		var found []string
		for ; iter.Valid(); iter.Next() {
			found = append(found, string(iter.Key()))
		}
		require.Equal(t, []string{"a", "c", "d", "e"}, found)
	})
}

func TestEVMDatabaseVersionEdgeCases(t *testing.T) {
	dir, err := os.MkdirTemp("", "evm_edge_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	db, err := OpenDB(dir, StoreStorage)
	require.NoError(t, err)
	defer db.Close()

	t.Run("Query before any version exists", func(t *testing.T) {
		key := []byte("future_key")
		err := db.Set(key, []byte("value"), 100)
		require.NoError(t, err)

		// Query at version 50 - should not find
		got, err := db.Get(key, 50)
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("Empty database", func(t *testing.T) {
		got, err := db.Get([]byte("nonexistent"), 1)
		require.NoError(t, err)
		require.Nil(t, got)

		has, err := db.Has([]byte("nonexistent"), 1)
		require.NoError(t, err)
		require.False(t, has)
	})

	t.Run("Latest version tracking", func(t *testing.T) {
		require.Equal(t, int64(100), db.GetLatestVersion())

		err := db.Set([]byte("new_key"), []byte("value"), 200)
		require.NoError(t, err)
		require.Equal(t, int64(200), db.GetLatestVersion())
	})
}

func TestParseKey(t *testing.T) {
	// Tests that we correctly integrate with commonevm.ParseEVMKey

	t.Run("Parse storage key", func(t *testing.T) {
		// StateKeyPrefix = 0x03, addr (20 bytes) + slot (32 bytes)
		addr := make([]byte, 20)
		slot := make([]byte, 32)
		key := append([]byte{0x03}, append(addr, slot...)...)

		storeType, stripped := commonevm.ParseEVMKey(key)
		require.Equal(t, StoreStorage, storeType)
		require.Equal(t, append(addr, slot...), stripped)
	})

	t.Run("Parse nonce key", func(t *testing.T) {
		// NonceKeyPrefix = 0x0a, addr (20 bytes)
		addr := make([]byte, 20)
		key := append([]byte{0x0a}, addr...)

		storeType, stripped := commonevm.ParseEVMKey(key)
		require.Equal(t, StoreNonce, storeType)
		require.Equal(t, addr, stripped)
	})

	t.Run("Parse code key", func(t *testing.T) {
		// CodeKeyPrefix = 0x07, addr (20 bytes)
		addr := make([]byte, 20)
		key := append([]byte{0x07}, addr...)

		storeType, stripped := commonevm.ParseEVMKey(key)
		require.Equal(t, StoreCode, storeType)
		require.Equal(t, addr, stripped)
	})

	t.Run("Unknown key prefix goes to legacy", func(t *testing.T) {
		key := []byte{0xff, 0x01, 0x02}

		storeType, keyBytes := commonevm.ParseEVMKey(key)
		require.Equal(t, StoreLegacy, storeType)
		require.Equal(t, key, keyBytes) // Legacy keys keep full key
	})

	t.Run("Malformed key goes to legacy", func(t *testing.T) {
		// Storage key needs prefix + 20 + 32 bytes
		key := []byte{0x03, 0x01, 0x02} // too short

		storeType, keyBytes := commonevm.ParseEVMKey(key)
		require.Equal(t, StoreLegacy, storeType)
		require.Equal(t, key, keyBytes) // Malformed keys go to legacy with full key
	})

	t.Run("Parse codesize key goes to legacy", func(t *testing.T) {
		// CodeSizeKeyPrefix = 0x09, addr (20 bytes)
		// CodeSize is routed to legacy store (not its own optimized DB)
		addr := make([]byte, 20)
		addr[0] = 0x42
		key := append([]byte{0x09}, addr...)

		storeType, keyBytes := commonevm.ParseEVMKey(key)
		require.Equal(t, StoreLegacy, storeType)
		require.Equal(t, key, keyBytes) // Legacy preserves the full key
	})
}

func TestCodeSizeGoesToLegacyDB(t *testing.T) {
	dir, err := os.MkdirTemp("", "evm_codesize_legacy_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewEVMStateStore(dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	// CodeSize should be routed to the Legacy DB, not its own DB
	legacyDB := store.GetDB(StoreLegacy)
	require.NotNil(t, legacyDB, "Legacy database should exist")

	// Write a codesize key through the store (using full key since it goes to legacy)
	addr := make([]byte, 20)
	addr[0] = 0x42
	codeSizeKey := append([]byte{0x09}, addr...)
	err = legacyDB.Set(codeSizeKey, []byte{0x00, 0x10}, 1)
	require.NoError(t, err)

	val, err := legacyDB.Get(codeSizeKey, 1)
	require.NoError(t, err)
	require.Equal(t, []byte{0x00, 0x10}, val)
}

func TestPrune(t *testing.T) {
	dir, err := os.MkdirTemp("", "evm_prune_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	db, err := OpenDB(dir, StoreStorage)
	require.NoError(t, err)
	defer db.Close()

	// Write at versions 1, 5, 10
	key := []byte("prune_key")
	err = db.Set(key, []byte("v1"), 1)
	require.NoError(t, err)
	err = db.Set(key, []byte("v5"), 5)
	require.NoError(t, err)
	err = db.Set(key, []byte("v10"), 10)
	require.NoError(t, err)

	// Prune versions < 5
	err = db.Prune(5)
	require.NoError(t, err)

	// v1 should be gone
	got, err := db.Get(key, 1)
	require.NoError(t, err)
	require.Nil(t, got)

	// v5 should still exist
	got, err = db.Get(key, 5)
	require.NoError(t, err)
	require.Equal(t, []byte("v5"), got)

	// v10 should still exist
	got, err = db.Get(key, 10)
	require.NoError(t, err)
	require.Equal(t, []byte("v10"), got)

	// Earliest version should be updated
	require.Equal(t, int64(5), db.GetEarliestVersion())
}
