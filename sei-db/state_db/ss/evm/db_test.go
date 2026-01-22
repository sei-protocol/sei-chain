package evm

import (
	"os"
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
