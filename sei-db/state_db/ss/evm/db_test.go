package evm

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

func TestEVMDatabase(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evm_db_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	db, err := OpenEVMDB(tmpDir, StorageStore)
	require.NoError(t, err)
	defer db.Close()

	t.Run("Set and Get", func(t *testing.T) {
		key := []byte("test_key")
		value := []byte("test_value")

		err := db.Set(key, value, 1)
		require.NoError(t, err)

		got, err := db.Get(key, 1)
		require.NoError(t, err)
		require.Equal(t, value, got)
	})

	t.Run("Version handling", func(t *testing.T) {
		key := []byte("versioned_key")

		// Write at version 1
		err := db.Set(key, []byte("v1"), 1)
		require.NoError(t, err)

		// Write at version 5
		err = db.Set(key, []byte("v5"), 5)
		require.NoError(t, err)

		// Read at version 3 should return v1
		val, err := db.Get(key, 3)
		require.NoError(t, err)
		require.Equal(t, []byte("v1"), val)

		// Read at version 5 should return v5
		val, err = db.Get(key, 5)
		require.NoError(t, err)
		require.Equal(t, []byte("v5"), val)

		// Read at version 10 should return v5
		val, err = db.Get(key, 10)
		require.NoError(t, err)
		require.Equal(t, []byte("v5"), val)
	})

	t.Run("Delete (tombstone)", func(t *testing.T) {
		key := []byte("delete_key")

		err := db.Set(key, []byte("exists"), 1)
		require.NoError(t, err)

		err = db.Delete(key, 2)
		require.NoError(t, err)

		// At version 1, key should exist
		val, err := db.Get(key, 1)
		require.NoError(t, err)
		require.Equal(t, []byte("exists"), val)

		// At version 2, key should be deleted
		val, err = db.Get(key, 2)
		require.NoError(t, err)
		require.Nil(t, val)
	})

	t.Run("Has", func(t *testing.T) {
		key := []byte("has_key")

		has, err := db.Has(key, 1)
		require.NoError(t, err)
		require.False(t, has)

		err = db.Set(key, []byte("value"), 1)
		require.NoError(t, err)

		has, err = db.Has(key, 1)
		require.NoError(t, err)
		require.True(t, has)
	})
}

func TestEVMStateStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evm_state_store_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	store, err := NewEVMStateStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	t.Run("Multiple store types", func(t *testing.T) {
		// Storage
		err := store.Set(StorageStore, []byte("storage_key"), []byte("storage_val"), 1)
		require.NoError(t, err)

		// Code
		err = store.Set(CodeStore, []byte("code_key"), []byte("bytecode"), 1)
		require.NoError(t, err)

		// Nonce
		err = store.Set(NonceStore, []byte("nonce_key"), []byte{5}, 1)
		require.NoError(t, err)

		// Verify
		val, err := store.Get(StorageStore, []byte("storage_key"), 1)
		require.NoError(t, err)
		require.Equal(t, []byte("storage_val"), val)

		val, err = store.Get(CodeStore, []byte("code_key"), 1)
		require.NoError(t, err)
		require.Equal(t, []byte("bytecode"), val)

		val, err = store.Get(NonceStore, []byte("nonce_key"), 1)
		require.NoError(t, err)
		require.Equal(t, []byte{5}, val)
	})

	t.Run("ApplyChangeset", func(t *testing.T) {
		changes := map[EVMStoreType][]*iavl.KVPair{
			StorageStore: {
				{Key: []byte("addr1"), Value: []byte("slot_value")},
			},
			CodeStore: {
				{Key: []byte("addr2"), Value: []byte("contract_code")},
			},
		}

		err := store.ApplyChangeset(2, changes)
		require.NoError(t, err)

		val, err := store.Get(StorageStore, []byte("addr1"), 2)
		require.NoError(t, err)
		require.Equal(t, []byte("slot_value"), val)

		val, err = store.Get(CodeStore, []byte("addr2"), 2)
		require.NoError(t, err)
		require.Equal(t, []byte("contract_code"), val)
	})
}

func TestKeyRouter(t *testing.T) {
	router := NewKeyRouter()

	t.Run("Routes EVM storage keys", func(t *testing.T) {
		key := append([]byte{0x03}, []byte("address_storage")...)
		storeType, strippedKey, isEVM := router.RouteKey(EVMStoreKey, key)

		require.True(t, isEVM)
		require.Equal(t, StorageStore, storeType)
		require.Equal(t, []byte("address_storage"), strippedKey)
	})

	t.Run("Routes EVM code keys", func(t *testing.T) {
		key := append([]byte{0x07}, []byte("contract_addr")...)
		storeType, strippedKey, isEVM := router.RouteKey(EVMStoreKey, key)

		require.True(t, isEVM)
		require.Equal(t, CodeStore, storeType)
		require.Equal(t, []byte("contract_addr"), strippedKey)
	})

	t.Run("Routes EVM nonce keys", func(t *testing.T) {
		key := append([]byte{0x0a}, []byte("account_addr")...)
		storeType, strippedKey, isEVM := router.RouteKey(EVMStoreKey, key)

		require.True(t, isEVM)
		require.Equal(t, NonceStore, storeType)
		require.Equal(t, []byte("account_addr"), strippedKey)
	})

	t.Run("Non-routed EVM keys", func(t *testing.T) {
		// Other EVM key prefix
		key := []byte{0x01, 'a', 'b', 'c'}
		_, _, isEVM := router.RouteKey(EVMStoreKey, key)
		require.False(t, isEVM)
	})

	t.Run("Non-EVM store", func(t *testing.T) {
		key := []byte("some_key")
		_, _, isEVM := router.RouteKey("bank", key)
		require.False(t, isEVM)
	})

	t.Run("RestoreKey", func(t *testing.T) {
		strippedKey := []byte("test_data")

		restored := router.RestoreKey(StorageStore, strippedKey)
		require.Equal(t, append([]byte{0x03}, strippedKey...), restored)

		restored = router.RestoreKey(CodeStore, strippedKey)
		require.Equal(t, append([]byte{0x07}, strippedKey...), restored)

		restored = router.RestoreKey(NonceStore, strippedKey)
		require.Equal(t, append([]byte{0x0a}, strippedKey...), restored)
	})
}

func TestEVMDatabaseBatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evm_batch_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	db, err := OpenEVMDB(tmpDir, StorageStore)
	require.NoError(t, err)
	defer db.Close()

	t.Run("ApplyBatch multiple keys", func(t *testing.T) {
		pairs := []*iavl.KVPair{
			{Key: []byte("key1"), Value: []byte("val1")},
			{Key: []byte("key2"), Value: []byte("val2")},
			{Key: []byte("key3"), Value: []byte("val3")},
		}

		err := db.ApplyBatch(pairs, 1)
		require.NoError(t, err)

		for _, pair := range pairs {
			val, err := db.Get(pair.Key, 1)
			require.NoError(t, err)
			require.Equal(t, pair.Value, val)
		}
	})

	t.Run("ApplyBatch with deletes", func(t *testing.T) {
		// First set some values
		pairs := []*iavl.KVPair{
			{Key: []byte("del_key1"), Value: []byte("val1")},
			{Key: []byte("del_key2"), Value: []byte("val2")},
		}
		err := db.ApplyBatch(pairs, 2)
		require.NoError(t, err)

		// Now delete one
		deletePairs := []*iavl.KVPair{
			{Key: []byte("del_key1"), Delete: true},
			{Key: []byte("del_key2"), Value: []byte("updated")},
		}
		err = db.ApplyBatch(deletePairs, 3)
		require.NoError(t, err)

		// Check del_key1 is deleted at version 3
		val, err := db.Get([]byte("del_key1"), 3)
		require.NoError(t, err)
		require.Nil(t, val)

		// Check del_key1 still exists at version 2
		val, err = db.Get([]byte("del_key1"), 2)
		require.NoError(t, err)
		require.Equal(t, []byte("val1"), val)

		// Check del_key2 is updated
		val, err = db.Get([]byte("del_key2"), 3)
		require.NoError(t, err)
		require.Equal(t, []byte("updated"), val)
	})

	t.Run("ApplyBatch empty", func(t *testing.T) {
		err := db.ApplyBatch([]*iavl.KVPair{}, 4)
		require.NoError(t, err)
	})
}

func TestEVMStateStoreParallel(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evm_parallel_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	store, err := NewEVMStateStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	t.Run("ApplyChangesetParallel", func(t *testing.T) {
		changes := map[EVMStoreType][]*iavl.KVPair{
			StorageStore: {
				{Key: []byte("storage1"), Value: []byte("s1")},
				{Key: []byte("storage2"), Value: []byte("s2")},
			},
			CodeStore: {
				{Key: []byte("code1"), Value: []byte("bytecode1")},
			},
			NonceStore: {
				{Key: []byte("nonce1"), Value: []byte{1}},
			},
		}

		err := store.ApplyChangesetParallel(1, changes)
		require.NoError(t, err)

		// Verify all writes
		val, err := store.Get(StorageStore, []byte("storage1"), 1)
		require.NoError(t, err)
		require.Equal(t, []byte("s1"), val)

		val, err = store.Get(CodeStore, []byte("code1"), 1)
		require.NoError(t, err)
		require.Equal(t, []byte("bytecode1"), val)

		val, err = store.Get(NonceStore, []byte("nonce1"), 1)
		require.NoError(t, err)
		require.Equal(t, []byte{1}, val)
	})
}

func TestEVMDatabaseConcurrent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evm_concurrent_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	db, err := OpenEVMDB(tmpDir, StorageStore)
	require.NoError(t, err)
	defer db.Close()

	// Pre-populate some data
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		val := []byte(fmt.Sprintf("val%d", i))
		require.NoError(t, db.Set(key, val, 1))
	}

	t.Run("Concurrent reads", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					key := []byte(fmt.Sprintf("key%d", j))
					_, err := db.Get(key, 1)
					require.NoError(t, err)
				}
			}(i)
		}
		wg.Wait()
	})

	t.Run("Concurrent reads and writes", func(t *testing.T) {
		var wg sync.WaitGroup

		// Readers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					key := []byte(fmt.Sprintf("key%d", j))
					_, _ = db.Get(key, 1)
				}
			}()
		}

		// Writers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					key := []byte(fmt.Sprintf("concurrent_key_%d_%d", idx, j))
					val := []byte(fmt.Sprintf("val_%d_%d", idx, j))
					_ = db.Set(key, val, int64(2+idx))
				}
			}(i)
		}

		wg.Wait()
	})
}

func TestEVMDatabaseIterator(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evm_iter_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	db, err := OpenEVMDB(tmpDir, StorageStore)
	require.NoError(t, err)
	defer db.Close()

	// Add data
	keys := []string{"aaa", "bbb", "ccc", "ddd"}
	for i, k := range keys {
		require.NoError(t, db.Set([]byte(k), []byte(fmt.Sprintf("val%d", i)), 1))
	}

	t.Run("Forward iteration", func(t *testing.T) {
		itr, err := db.Iterator(nil, nil, 1)
		require.NoError(t, err)
		defer itr.Close()

		var found []string
		for ; itr.Valid(); itr.Next() {
			found = append(found, string(itr.Key()))
		}
		require.Equal(t, keys, found)
	})

	t.Run("Reverse iteration", func(t *testing.T) {
		itr, err := db.ReverseIterator(nil, nil, 1)
		require.NoError(t, err)
		defer itr.Close()

		var found []string
		for ; itr.Valid(); itr.Next() {
			found = append(found, string(itr.Key()))
		}

		// Should be reversed
		expected := []string{"ddd", "ccc", "bbb", "aaa"}
		require.Equal(t, expected, found)
	})

	t.Run("Bounded iteration", func(t *testing.T) {
		itr, err := db.Iterator([]byte("bbb"), []byte("ddd"), 1)
		require.NoError(t, err)
		defer itr.Close()

		var found []string
		for ; itr.Valid(); itr.Next() {
			found = append(found, string(itr.Key()))
		}
		require.Equal(t, []string{"bbb", "ccc"}, found)
	})
}

func TestEVMDatabaseVersionEdgeCases(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "evm_version_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	db, err := OpenEVMDB(tmpDir, StorageStore)
	require.NoError(t, err)
	defer db.Close()

	t.Run("Version 0 read", func(t *testing.T) {
		require.NoError(t, db.Set([]byte("key"), []byte("val"), 1))

		// Version 0 should not find the key (written at version 1)
		val, err := db.Get([]byte("key"), 0)
		require.NoError(t, err)
		require.Nil(t, val)
	})

	t.Run("Large version gap", func(t *testing.T) {
		require.NoError(t, db.Set([]byte("gapkey"), []byte("v1"), 1))
		require.NoError(t, db.Set([]byte("gapkey"), []byte("v1000"), 1000))

		// Version 500 should return v1
		val, err := db.Get([]byte("gapkey"), 500)
		require.NoError(t, err)
		require.Equal(t, []byte("v1"), val)

		// Version 1000 should return v1000
		val, err = db.Get([]byte("gapkey"), 1000)
		require.NoError(t, err)
		require.Equal(t, []byte("v1000"), val)
	})

	t.Run("Overwrite same version", func(t *testing.T) {
		// This shouldn't happen in practice, but let's be safe
		require.NoError(t, db.Set([]byte("owkey"), []byte("first"), 5))
		require.NoError(t, db.Set([]byte("owkey"), []byte("second"), 5))

		// Should return the last written value
		val, err := db.Get([]byte("owkey"), 5)
		require.NoError(t, err)
		require.Equal(t, []byte("second"), val)
	})

	t.Run("Empty value vs tombstone", func(t *testing.T) {
		// Set with empty value (should be treated as tombstone)
		require.NoError(t, db.Set([]byte("emptykey"), []byte{}, 1))

		// Get should return nil (empty is tombstone)
		val, err := db.Get([]byte("emptykey"), 1)
		require.NoError(t, err)
		require.Nil(t, val)
	})
}
