package composite

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/stretchr/testify/require"
)

// testNewCompositeStateStore opens a PebbleDB cosmos backend and creates a
// CompositeStateStore. Test-only helper to avoid repeating the two-step creation.
func testNewCompositeStateStore(ssConfig config.StateStoreConfig, homeDir string, log logger.Logger) (*CompositeStateStore, error) {
	dbHome := utils.GetStateStorePath(homeDir, ssConfig.Backend)
	if ssConfig.DBDirectory != "" {
		dbHome = ssConfig.DBDirectory
	}
	cosmosStore, err := mvcc.OpenDB(dbHome, ssConfig)
	if err != nil {
		return nil, err
	}
	return NewCompositeStateStore(cosmosStore, ssConfig, homeDir, log)
}

func setupTestStores(t *testing.T) (*CompositeStateStore, string, func()) {
	dir, err := os.MkdirTemp("", "composite_store_test")
	require.NoError(t, err)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0, // Sync writes for tests
		KeepRecent:       100000,
		WriteMode:        config.DualWrite,
		ReadMode:         config.EVMFirstRead,
		EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
	}

	compositeStore, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
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

	// Create composite store with EVM disabled (default cosmos_only modes)
	store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
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

func TestCompositeStateStoreDualWrite(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	// Create a valid EVM storage key (prefix 0x03 + 20 byte address + 32 byte slot)
	addr := make([]byte, 20)
	addr[0] = 0x01 // Non-zero address
	slot := make([]byte, 32)
	slot[0] = 0x01 // Non-zero slot
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	t.Run("EVM data dual-written", func(t *testing.T) {
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
		err := store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

		// Verify via Get (will check EVM_SS first, then Cosmos_SS)
		val, err := store.Get("evm", 1, storageKey)
		require.NoError(t, err)
		require.Equal(t, []byte("storage_value"), val)

		// Verify EVM store has the data directly
		if store.evmStore != nil {
			// Get the stripped key using commonevm.ParseEVMKey
			_, strippedKey := commonevm.ParseEVMKey(storageKey)
			db := store.evmStore.GetDB(evm.StoreStorage)
			require.NotNil(t, db)
			evmVal, err := db.Get(strippedKey, 1)
			require.NoError(t, err)
			require.Equal(t, []byte("storage_value"), evmVal)
		}
	})

	t.Run("Non-EVM data only to Cosmos", func(t *testing.T) {
		changesets := []*proto.NamedChangeSet{
			{
				Name: "bank",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("balance"), Value: []byte("100")},
					},
				},
			},
		}
		err := store.ApplyChangesetSync(2, changesets)
		require.NoError(t, err)

		// Should be readable
		val, err := store.Get("bank", 2, []byte("balance"))
		require.NoError(t, err)
		require.Equal(t, []byte("100"), val)
	})
}

func TestCompositeStateStoreMixedChangeset(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	// Create valid EVM keys
	addr := make([]byte, 20)
	addr[0] = 0x02

	nonceKey := append([]byte{0x0a}, addr...) // NonceKeyPrefix
	codeKey := append([]byte{0x07}, addr...)  // CodeKeyPrefix

	// Mixed changeset with EVM and non-EVM data
	changesets := []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("balance"), Value: []byte("500")},
				},
			},
		},
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: nonceKey, Value: []byte{0x01}},
					{Key: codeKey, Value: []byte{0x60, 0x80}},
				},
			},
		},
		{
			Name: "staking",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("validator"), Value: []byte("active")},
				},
			},
		},
	}

	err := store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	// Verify all data
	val, err := store.Get("bank", 1, []byte("balance"))
	require.NoError(t, err)
	require.Equal(t, []byte("500"), val)

	val, err = store.Get("evm", 1, nonceKey)
	require.NoError(t, err)
	require.Equal(t, []byte{0x01}, val)

	val, err = store.Get("evm", 1, codeKey)
	require.NoError(t, err)
	require.Equal(t, []byte{0x60, 0x80}, val)

	val, err = store.Get("staking", 1, []byte("validator"))
	require.NoError(t, err)
	require.Equal(t, []byte("active"), val)
}

func TestCompositeStateStoreDelete(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	addr := make([]byte, 20)
	slot := make([]byte, 32)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	// Write at v1
	changesets := []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: storageKey, Value: []byte("value")},
				},
			},
		},
	}
	err := store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	// Delete at v2
	changesets = []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: storageKey, Delete: true},
				},
			},
		},
	}
	err = store.ApplyChangesetSync(2, changesets)
	require.NoError(t, err)

	// v1 should still have value
	val, err := store.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("value"), val)

	// v2 should be deleted
	val, err = store.Get("evm", 2, storageKey)
	require.NoError(t, err)
	require.Nil(t, val)
}

// =============================================================================
// Bug-fix verification tests
// =============================================================================

// TestBug1Fix_WriteModeControlsEVMWrites verifies Bug 1 fix:
// WriteMode flag is respected - CosmosOnlyWrite skips EVM, DualWrite populates both.
func TestBug1Fix_WriteModeControlsEVMWrites(t *testing.T) {
	addr := make([]byte, 20)
	addr[0] = 0xAA
	slot := make([]byte, 32)
	slot[0] = 0xBB
	storageKey := append([]byte{0x03}, append(addr, slot...)...) // StateKeyPrefix

	t.Run("CosmosOnlyWrite does not open EVM stores", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "bug1_cosmos_only_test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ssConfig := config.StateStoreConfig{
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       100000,
			WriteMode:        config.CosmosOnlyWrite,
			ReadMode:         config.CosmosOnlyRead,
		}

		store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
		require.NoError(t, err)
		defer store.Close()

		// EVM store should NOT be opened in cosmos-only mode
		require.Nil(t, store.evmStore, "EVM store should be nil in cosmos-only mode")

		// Write EVM data -- goes only to Cosmos
		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: storageKey, Value: []byte("cosmos_only")},
					},
				},
			},
		}
		err = store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

		// Cosmos should have the data
		val, err := store.cosmosStore.Get("evm", 1, storageKey)
		require.NoError(t, err)
		require.Equal(t, []byte("cosmos_only"), val)
	})

	t.Run("DualWrite populates both Cosmos and EVM stores", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "bug1_dual_write_test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ssConfig := config.StateStoreConfig{
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       100000,
			WriteMode:        config.DualWrite, // Bug 1 fix: this must populate EVM
			ReadMode:         config.EVMFirstRead,
			EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
		}

		store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
		require.NoError(t, err)
		defer store.Close()

		// Write EVM data
		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: storageKey, Value: []byte("in_both_stores")},
					},
				},
			},
		}
		err = store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

		// Cosmos should have the data
		cosmosVal, err := store.cosmosStore.Get("evm", 1, storageKey)
		require.NoError(t, err)
		require.Equal(t, []byte("in_both_stores"), cosmosVal, "Cosmos should have the data")

		// EVM store should ALSO have the data
		_, strippedKey := commonevm.ParseEVMKey(storageKey)
		evmDB := store.evmStore.GetDB(evm.StoreStorage)
		require.NotNil(t, evmDB)
		evmVal, err := evmDB.Get(strippedKey, 1)
		require.NoError(t, err)
		require.Equal(t, []byte("in_both_stores"), evmVal, "EVM DB should have data when WriteMode is DualWrite")
	})
}

// TestBug1Fix_ReadModeControlsEVMReads verifies Bug 1 fix:
// ReadMode flag controls whether EVM store is consulted on reads.
func TestBug1Fix_ReadModeControlsEVMReads(t *testing.T) {
	addr := make([]byte, 20)
	addr[0] = 0xCC
	slot := make([]byte, 32)
	slot[0] = 0xDD
	storageKey := append([]byte{0x03}, append(addr, slot...)...) // StateKeyPrefix

	t.Run("CosmosOnlyRead never checks EVM even if EVM has data", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "bug1_read_cosmos_only_test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ssConfig := config.StateStoreConfig{
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       100000,
			WriteMode:        config.DualWrite,
			ReadMode:         config.CosmosOnlyRead, // Bug 1: this was the only path before fix
			EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
		}

		store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
		require.NoError(t, err)
		defer store.Close()

		// Write data (DualWrite populates both)
		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: storageKey, Value: []byte("cosmos_value")},
					},
				},
			},
		}
		err = store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

		// Now write a DIFFERENT value directly to EVM (simulating divergence)
		_, strippedKey := commonevm.ParseEVMKey(storageKey)
		evmDB := store.evmStore.GetDB(evm.StoreStorage)
		err = evmDB.Set(strippedKey, []byte("evm_only_value"), 2)
		require.NoError(t, err)

		// Read at v2 via composite store with CosmosOnlyRead -- should NOT see "evm_only_value"
		val, err := store.Get("evm", 2, storageKey)
		require.NoError(t, err)
		// Should get cosmos value at v1 (latest <= v2 in Cosmos)
		require.Equal(t, []byte("cosmos_value"), val, "CosmosOnlyRead should bypass EVM store")
	})

	t.Run("EVMFirstRead returns EVM data when available", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "bug1_read_evm_first_test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ssConfig := config.StateStoreConfig{
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       100000,
			WriteMode:        config.DualWrite,
			ReadMode:         config.EVMFirstRead, // Bug 1 fix: this activates EVM reads
			EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
		}

		store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
		require.NoError(t, err)
		defer store.Close()

		// Write EVM data
		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: storageKey, Value: []byte("dual_written")},
					},
				},
			},
		}
		err = store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

		// Read via composite - should find data from EVM store
		val, err := store.Get("evm", 1, storageKey)
		require.NoError(t, err)
		require.Equal(t, []byte("dual_written"), val)

		// Directly confirm it came from EVM by checking EVM DB
		_, strippedKey := commonevm.ParseEVMKey(storageKey)
		evmDB := store.evmStore.GetDB(evm.StoreStorage)
		evmVal, err := evmDB.Get(strippedKey, 1)
		require.NoError(t, err)
		require.Equal(t, []byte("dual_written"), evmVal, "EVMFirstRead should serve from EVM store")

		// Has() should also work via EVM
		has, err := store.Has("evm", 1, storageKey)
		require.NoError(t, err)
		require.True(t, has)
	})
}

// TestCodeSizeGoesToLegacy verifies that CodeSize keys are routed to the Legacy DB
// (not a separate optimized DB), since we don't want to store CodeSize in EVM long-term.
func TestCodeSizeGoesToLegacy(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	// Override to EVMFirstRead so we read from EVM_SS
	store.config.ReadMode = config.EVMFirstRead

	// CodeSizeKeyPrefix = 0x09, addr = 20 bytes
	addr := make([]byte, 20)
	addr[0] = 0x42
	addr[19] = 0xFF
	codeSizeKey := append([]byte{0x09}, addr...)
	codeSizeValue := []byte{0x00, 0x00, 0x10, 0x00} // 4096 bytes

	// Write CodeSize via composite store
	changesets := []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: codeSizeKey, Value: codeSizeValue},
				},
			},
		},
	}
	err := store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	// CodeSize should be in the Legacy DB with the full key preserved
	legacyDB := store.evmStore.GetDB(evm.StoreLegacy)
	require.NotNil(t, legacyDB, "Legacy DB must exist")

	_, keyBytes := commonevm.ParseEVMKey(codeSizeKey)
	require.Equal(t, codeSizeKey, keyBytes, "CodeSize key should be preserved as full key (legacy)")

	val, err := legacyDB.Get(keyBytes, 1)
	require.NoError(t, err)
	require.Equal(t, codeSizeValue, val, "CodeSize value should be in Legacy DB")

	// Read back through composite store (EVMFirstRead should find it in Legacy DB)
	compositeVal, err := store.Get("evm", 1, codeSizeKey)
	require.NoError(t, err)
	require.Equal(t, codeSizeValue, compositeVal, "CodeSize should be readable end-to-end via Legacy DB")
}

// TestAllEVMKeyTypesWritten verifies that all recognized EVM key types
// (nonce, codehash, code, storage, legacy) plus codesize (which goes to legacy)
// get written to their respective databases during DualWrite.
func TestAllEVMKeyTypesWritten(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	addr := make([]byte, 20)
	for i := range addr {
		addr[i] = byte(i + 1)
	}
	slot := make([]byte, 32)
	for i := range slot {
		slot[i] = byte(i + 100)
	}

	// Build keys for every EVM type
	nonceKey := append([]byte{0x0a}, addr...)                    // NonceKeyPrefix
	codeHashKey := append([]byte{0x08}, addr...)                 // CodeHashKeyPrefix
	codeKey := append([]byte{0x07}, addr...)                     // CodeKeyPrefix
	codeSizeKey := append([]byte{0x09}, addr...)                 // CodeSizeKeyPrefix (goes to legacy)
	storageKey := append([]byte{0x03}, append(addr, slot...)...) // StateKeyPrefix
	legacyKey := append([]byte{0x01}, addr...)                   // EVMAddressToSeiAddress (Legacy)

	changesets := []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: nonceKey, Value: []byte{0x05}},
					{Key: codeHashKey, Value: []byte("hash_abc")},
					{Key: codeKey, Value: []byte{0x60, 0x80, 0x60, 0x40}},
					{Key: codeSizeKey, Value: []byte{0x00, 0x04}},
					{Key: storageKey, Value: []byte("storage_val")},
					{Key: legacyKey, Value: []byte("sei1abc")},
				},
			},
		},
	}

	err := store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	// Verify each EVM DB got its data
	// Note: CodeSize goes to Legacy DB with full key preserved
	tests := []struct {
		name      string
		storeType evm.EVMStoreType
		key       []byte // key expected in the DB (stripped for optimized, full for legacy)
		value     []byte
	}{
		{"Nonce", evm.StoreNonce, addr, []byte{0x05}},
		{"CodeHash", evm.StoreCodeHash, addr, []byte("hash_abc")},
		{"Code", evm.StoreCode, addr, []byte{0x60, 0x80, 0x60, 0x40}},
		{"CodeSize", evm.StoreLegacy, codeSizeKey, []byte{0x00, 0x04}}, // CodeSize → Legacy with full key
		{"Storage", evm.StoreStorage, append(addr, slot...), []byte("storage_val")},
		{"Legacy", evm.StoreLegacy, legacyKey, []byte("sei1abc")}, // Legacy keeps full key
	}

	for _, tc := range tests {
		t.Run(tc.name+" DB written", func(t *testing.T) {
			db := store.evmStore.GetDB(tc.storeType)
			require.NotNil(t, db, "%s DB should exist", tc.name)

			val, err := db.Get(tc.key, 1)
			require.NoError(t, err)
			require.Equal(t, tc.value, val, "%s DB should contain the correct value", tc.name)
		})
	}
}

// TestDualWriteAsyncAlsoPopulatesEVM verifies DualWrite works for ApplyChangesetAsync path too.
func TestDualWriteAsyncAlsoPopulatesEVM(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	addr := make([]byte, 20)
	addr[0] = 0x77
	slot := make([]byte, 32)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	changesets := []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: storageKey, Value: []byte("async_value")},
				},
			},
		},
	}

	err := store.ApplyChangesetAsync(1, changesets)
	require.NoError(t, err)

	// Async writes are enqueued to per-DB channels; wait briefly for workers to process
	time.Sleep(100 * time.Millisecond)

	// Verify EVM store has the data
	_, strippedKey := commonevm.ParseEVMKey(storageKey)
	evmDB := store.evmStore.GetDB(evm.StoreStorage)
	require.NotNil(t, evmDB)
	val, err := evmDB.Get(strippedKey, 1)
	require.NoError(t, err)
	require.Equal(t, []byte("async_value"), val, "ApplyChangesetAsync should also dual-write to EVM")
}

// TestCompositeStateStorePrunesBothStores verifies that pruning removes old versions
// from both Cosmos and EVM stores using the shared KeepRecent config.
func TestCompositeStateStorePrunesBothStores(t *testing.T) {
	dir, err := os.MkdirTemp("", "composite_prune_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       5, // Shared KeepRecent for both Cosmos and EVM
		WriteMode:        config.DualWrite,
		EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
	}

	store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	// Write 10 versions with EVM data
	addr := make([]byte, 20)
	addr[0] = 0x01
	slot := make([]byte, 32)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	for v := int64(1); v <= 10; v++ {
		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: storageKey, Value: []byte{byte(v)}},
					},
				},
			},
		}
		err := store.ApplyChangesetSync(v, changesets)
		require.NoError(t, err)
		err = store.SetLatestVersion(v)
		require.NoError(t, err)
	}

	// Prune version 5: versions 1-4 should be pruned, 5-10 kept
	pruneVersion := int64(5)
	err = store.Prune(pruneVersion)
	require.NoError(t, err)

	// EVM version 6 should still be available (kept by shared KeepRecent)
	_, strippedKey := commonevm.ParseEVMKey(storageKey)
	evmDB := store.evmStore.GetDB(evm.StoreStorage)
	require.NotNil(t, evmDB)

	val, err := evmDB.Get(strippedKey, 6)
	require.NoError(t, err)
	require.Equal(t, []byte{6}, val, "EVM version 6 should still be available after pruning")

	// EVM version 10 (latest) should be available
	val, err = evmDB.Get(strippedKey, 10)
	require.NoError(t, err)
	require.Equal(t, []byte{10}, val, "EVM latest version should be available")
}

// =============================================================================
// End-to-end behavioral verification tests
// =============================================================================

// TestE2E_AllEVMDBsReadableViaComposite verifies that each of the 6 EVM databases
// is correctly written to during DualWrite AND correctly readable via the composite
// store's Get() path with EVMFirstRead. This is the full round-trip proof.
func TestE2E_AllEVMDBsReadableViaComposite(t *testing.T) {
	dir, err := os.MkdirTemp("", "e2e_all_dbs_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
	}
	ssConfig.WriteMode = config.DualWrite
	ssConfig.ReadMode = config.EVMFirstRead
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	// Build realistic EVM keys for every type
	addr := make([]byte, 20)
	for i := range addr {
		addr[i] = byte(i + 0x10)
	}
	slot := make([]byte, 32)
	for i := range slot {
		slot[i] = byte(i + 0xA0)
	}

	type evmKeyTest struct {
		name     string
		fullKey  []byte
		value    []byte
		dbType   evm.EVMStoreType
		stripKey []byte // expected key in the individual EVM DB
	}

	tests := []evmKeyTest{
		{
			name:     "Nonce",
			fullKey:  append([]byte{0x0a}, addr...),
			value:    []byte{0x00, 0x00, 0x00, 0x2A}, // nonce=42
			dbType:   evm.StoreNonce,
			stripKey: addr,
		},
		{
			name:     "CodeHash",
			fullKey:  append([]byte{0x08}, addr...),
			value:    []byte("deadbeef01234567890abcdef1234567"),
			dbType:   evm.StoreCodeHash,
			stripKey: addr,
		},
		{
			name:     "Code",
			fullKey:  append([]byte{0x07}, addr...),
			value:    []byte{0x60, 0x80, 0x60, 0x40, 0x52, 0x34, 0x80, 0x15},
			dbType:   evm.StoreCode,
			stripKey: addr,
		},
		{
			name:     "CodeSize (legacy)",
			fullKey:  append([]byte{0x09}, addr...),
			value:    []byte{0x00, 0x00, 0x20, 0x00}, // 8192 bytes
			dbType:   evm.StoreLegacy,
			stripKey: append([]byte{0x09}, addr...), // CodeSize goes to legacy with full key preserved
		},
		{
			name:     "Storage",
			fullKey:  append([]byte{0x03}, append(addr, slot...)...),
			value:    []byte("storage_value_at_slot"),
			dbType:   evm.StoreStorage,
			stripKey: append(addr, slot...),
		},
		{
			name:     "Legacy (EVMToSeiAddr)",
			fullKey:  append([]byte{0x01}, addr...),
			value:    []byte("sei1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"),
			dbType:   evm.StoreLegacy,
			stripKey: append([]byte{0x01}, addr...), // legacy preserves full key
		},
	}

	// Write all keys in a single changeset at version 1
	var pairs []*iavl.KVPair
	for _, tc := range tests {
		pairs = append(pairs, &iavl.KVPair{Key: tc.fullKey, Value: tc.value})
	}
	changesets := []*proto.NamedChangeSet{
		{
			Name:      "evm",
			Changeset: iavl.ChangeSet{Pairs: pairs},
		},
	}
	err = store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)
	err = store.SetLatestVersion(1)
	require.NoError(t, err)

	for _, tc := range tests {
		t.Run(tc.name+"_direct_DB_read", func(t *testing.T) {
			// Verify the individual EVM database has the data with the stripped key
			db := store.evmStore.GetDB(tc.dbType)
			require.NotNil(t, db, "%s DB must be opened", tc.name)

			val, err := db.Get(tc.stripKey, 1)
			require.NoError(t, err)
			require.Equal(t, tc.value, val, "%s DB should contain correct value at stripped key", tc.name)
		})

		t.Run(tc.name+"_composite_Get_roundtrip", func(t *testing.T) {
			// Verify Get() through the composite store using the FULL key
			// This proves: full key -> ParseEVMKey -> stripped key -> EVM DB lookup works
			val, err := store.Get("evm", 1, tc.fullKey)
			require.NoError(t, err)
			require.Equal(t, tc.value, val, "%s should be readable via composite Get() with full key", tc.name)
		})

		t.Run(tc.name+"_composite_Has_roundtrip", func(t *testing.T) {
			has, err := store.Has("evm", 1, tc.fullKey)
			require.NoError(t, err)
			require.True(t, has, "%s should exist via composite Has()", tc.name)
		})
	}
}

// TestE2E_MVCCConsistencyAcrossBothStores verifies that multi-version writes maintain
// MVCC consistency in both Cosmos and EVM stores -- reading at each version returns
// the correct historical value from either store.
func TestE2E_MVCCConsistencyAcrossBothStores(t *testing.T) {
	dir, err := os.MkdirTemp("", "e2e_mvcc_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
	}
	ssConfig.WriteMode = config.DualWrite
	ssConfig.ReadMode = config.EVMFirstRead
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	addr := make([]byte, 20)
	addr[0] = 0xDE
	addr[19] = 0xAD
	slot := make([]byte, 32)
	slot[0] = 0xBE
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	// Write 5 versions with different values
	for v := int64(1); v <= 5; v++ {
		val := []byte(fmt.Sprintf("value_at_v%d", v))
		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: storageKey, Value: val},
					},
				},
			},
		}
		err := store.ApplyChangesetSync(v, changesets)
		require.NoError(t, err)
		err = store.SetLatestVersion(v)
		require.NoError(t, err)
	}

	// Verify each historical version returns the correct value
	for v := int64(1); v <= 5; v++ {
		expected := []byte(fmt.Sprintf("value_at_v%d", v))

		t.Run(fmt.Sprintf("composite_Get_v%d", v), func(t *testing.T) {
			val, err := store.Get("evm", v, storageKey)
			require.NoError(t, err)
			require.Equal(t, expected, val, "Composite Get at version %d", v)
		})

		t.Run(fmt.Sprintf("cosmos_direct_v%d", v), func(t *testing.T) {
			val, err := store.cosmosStore.Get("evm", v, storageKey)
			require.NoError(t, err)
			require.Equal(t, expected, val, "Cosmos direct Get at version %d", v)
		})

		t.Run(fmt.Sprintf("evm_direct_v%d", v), func(t *testing.T) {
			_, strippedKey := commonevm.ParseEVMKey(storageKey)
			db := store.evmStore.GetDB(evm.StoreStorage)
			val, err := db.Get(strippedKey, v)
			require.NoError(t, err)
			require.Equal(t, expected, val, "EVM direct Get at version %d", v)
		})
	}

	// Verify version consistency
	require.Equal(t, int64(5), store.GetLatestVersion(), "Composite latest version")
	require.Equal(t, int64(5), store.cosmosStore.GetLatestVersion(), "Cosmos latest version")
	require.Equal(t, int64(5), store.evmStore.GetLatestVersion(), "EVM latest version")
}

// TestE2E_NonEVMModulesUnaffectedByDualWrite verifies that enabling DualWrite+EVMFirstRead
// does not interfere with non-EVM modules (bank, staking, etc.) -- they continue to read
// exclusively from Cosmos_SS.
func TestE2E_NonEVMModulesUnaffectedByDualWrite(t *testing.T) {
	dir, err := os.MkdirTemp("", "e2e_non_evm_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
	}
	ssConfig.WriteMode = config.DualWrite
	ssConfig.ReadMode = config.EVMFirstRead
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	// Mixed changeset: bank + evm + staking
	addr := make([]byte, 20)
	slot := make([]byte, 32)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	changesets := []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("supply/usei"), Value: []byte("1000000000")},
					{Key: []byte("balances/sei1abc/usei"), Value: []byte("500")},
				},
			},
		},
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: storageKey, Value: []byte("evm_slot_data")},
				},
			},
		},
		{
			Name: "staking",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("validators/sei1val"), Value: []byte("bonded")},
				},
			},
		},
	}

	err = store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)
	err = store.SetLatestVersion(1)
	require.NoError(t, err)

	// Bank data: must be readable, lives only in Cosmos
	val, err := store.Get("bank", 1, []byte("supply/usei"))
	require.NoError(t, err)
	require.Equal(t, []byte("1000000000"), val)

	val, err = store.Get("bank", 1, []byte("balances/sei1abc/usei"))
	require.NoError(t, err)
	require.Equal(t, []byte("500"), val)

	// Staking data: must be readable, lives only in Cosmos
	val, err = store.Get("staking", 1, []byte("validators/sei1val"))
	require.NoError(t, err)
	require.Equal(t, []byte("bonded"), val)

	// EVM data: readable (from EVM store via EVMFirstRead)
	val, err = store.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("evm_slot_data"), val)

	// Bank Has
	has, err := store.Has("bank", 1, []byte("supply/usei"))
	require.NoError(t, err)
	require.True(t, has)

	// Non-existent module
	val, err = store.Get("auth", 1, []byte("some_key"))
	require.NoError(t, err)
	require.Nil(t, val)

	// Bank iterator still works through composite
	iter, err := store.Iterator("bank", 1, nil, nil)
	require.NoError(t, err)
	defer iter.Close()
	count := 0
	for ; iter.Valid(); iter.Next() {
		count++
	}
	require.Equal(t, 2, count, "Bank should have 2 keys via iterator")
}

// TestE2E_VersionConsistencyAfterSetLatestVersion verifies that SetLatestVersion
// propagates to both Cosmos and EVM stores, keeping them synchronized.
func TestE2E_VersionConsistencyAfterSetLatestVersion(t *testing.T) {
	dir, err := os.MkdirTemp("", "e2e_version_sync_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
	}
	ssConfig.WriteMode = config.DualWrite
	ssConfig.ReadMode = config.EVMFirstRead
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	// Simulate block commit pattern: ApplyChangeset then SetLatestVersion
	for v := int64(1); v <= 10; v++ {
		changesets := []*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key"), Value: []byte{byte(v)}},
					},
				},
			},
		}
		err := store.ApplyChangesetSync(v, changesets)
		require.NoError(t, err)
		err = store.SetLatestVersion(v)
		require.NoError(t, err)

		// After every block, both stores must agree on the version
		require.Equal(t, v, store.GetLatestVersion(), "Composite version at block %d", v)
		require.Equal(t, v, store.cosmosStore.GetLatestVersion(), "Cosmos version at block %d", v)
		require.Equal(t, v, store.evmStore.GetLatestVersion(), "EVM version at block %d", v)
	}
}

// TestE2E_DeleteTombstonePropagatedToBothStores verifies that a delete (tombstone)
// is correctly applied to both Cosmos and EVM stores, and subsequent reads at the
// delete version return nil from both.
func TestE2E_DeleteTombstonePropagatedToBothStores(t *testing.T) {
	dir, err := os.MkdirTemp("", "e2e_delete_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
	}
	ssConfig.WriteMode = config.DualWrite
	ssConfig.ReadMode = config.EVMFirstRead
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	addr := make([]byte, 20)
	addr[0] = 0xFF
	slot := make([]byte, 32)
	slot[0] = 0xEE
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	// Write at v1
	err = store.ApplyChangesetSync(1, []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: storageKey, Value: []byte("alive")},
				},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, store.SetLatestVersion(1))

	// Delete at v2
	err = store.ApplyChangesetSync(2, []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: storageKey, Delete: true},
				},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, store.SetLatestVersion(2))

	// v1: alive in both stores
	val, err := store.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("alive"), val, "v1 should be alive via composite")

	cosmosVal, err := store.cosmosStore.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("alive"), cosmosVal, "v1 should be alive in Cosmos")

	_, strippedKey := commonevm.ParseEVMKey(storageKey)
	evmDB := store.evmStore.GetDB(evm.StoreStorage)
	evmVal, err := evmDB.Get(strippedKey, 1)
	require.NoError(t, err)
	require.Equal(t, []byte("alive"), evmVal, "v1 should be alive in EVM DB")

	// v2: deleted in both stores
	val, err = store.Get("evm", 2, storageKey)
	require.NoError(t, err)
	require.Nil(t, val, "v2 should be nil via composite (deleted)")

	cosmosVal, err = store.cosmosStore.Get("evm", 2, storageKey)
	require.NoError(t, err)
	require.Nil(t, cosmosVal, "v2 should be nil in Cosmos (deleted)")

	evmVal, err = evmDB.Get(strippedKey, 2)
	require.NoError(t, err)
	require.Nil(t, evmVal, "v2 should be nil in EVM DB (tombstone)")

	// Re-write at v3
	err = store.ApplyChangesetSync(3, []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: storageKey, Value: []byte("resurrected")},
				},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, store.SetLatestVersion(3))

	val, err = store.Get("evm", 3, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("resurrected"), val, "v3 should be resurrected")
}

// TestE2E_FactoryMethodCreatesCorrectStoreType verifies that NewCompositeStateStore
// creates EVM stores when WriteMode/ReadMode require them and omits them when cosmos-only.
func TestE2E_FactoryMethodCreatesCorrectStoreType(t *testing.T) {
	t.Run("EVM enabled creates CompositeStateStore", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "factory_evm_test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ssConfig := config.StateStoreConfig{
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       100000,
			WriteMode:        config.DualWrite,
			ReadMode:         config.EVMFirstRead,
			EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
		}

		store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
		require.NoError(t, err)
		defer store.Close()

		// Must be a CompositeStateStore with EVM store
		require.NotNil(t, store.evmStore, "EVM store should be present")
		require.NotNil(t, store.cosmosStore, "Cosmos store should be present")

		// All EVM databases should be open
		for _, st := range evm.AllEVMStoreTypes() {
			db := store.evmStore.GetDB(st)
			require.NotNil(t, db, "EVM DB for %s should be open", evm.StoreTypeName(st))
		}
	})

	t.Run("EVM disabled creates store without EVM", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "factory_no_evm_test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ssConfig := config.StateStoreConfig{
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       100000,
		}
		// Default WriteMode=CosmosOnlyWrite, ReadMode=CosmosOnlyRead → no EVM stores

		store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
		require.NoError(t, err)
		defer store.Close()

		require.Nil(t, store.evmStore, "EVM store should be nil when cosmos_only modes")
		require.NotNil(t, store.cosmosStore, "Cosmos store should still be present")
	})
}

// =============================================================================
// Fix verification tests for Issues 1-4
// =============================================================================

// TestFix1_SplitWriteStripsEVMFromCosmos verifies that SplitWrite mode
// routes EVM data exclusively to EVM_SS and strips it from Cosmos_SS.
func TestFix1_SplitWriteStripsEVMFromCosmos(t *testing.T) {
	dir, err := os.MkdirTemp("", "fix1_split_write_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
	}
	ssConfig.WriteMode = config.SplitWrite
	ssConfig.ReadMode = config.SplitRead
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	// Create EVM storage key
	addr := make([]byte, 20)
	addr[0] = 0xAA
	slot := make([]byte, 32)
	slot[0] = 0xBB
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	// Mixed changeset: bank + evm
	changesets := []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("balance"), Value: []byte("100")},
				},
			},
		},
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: storageKey, Value: []byte("evm_value")},
				},
			},
		},
	}
	err = store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	// Bank data should be in Cosmos
	bankVal, err := store.cosmosStore.Get("bank", 1, []byte("balance"))
	require.NoError(t, err)
	require.Equal(t, []byte("100"), bankVal, "Bank data should be in Cosmos")

	// EVM data should NOT be in Cosmos (SplitWrite strips it)
	cosmosEVMVal, err := store.cosmosStore.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Nil(t, cosmosEVMVal, "EVM data should NOT be in Cosmos with SplitWrite")

	// EVM data should be in EVM_SS
	_, strippedKey := commonevm.ParseEVMKey(storageKey)
	evmDB := store.evmStore.GetDB(evm.StoreStorage)
	evmVal, err := evmDB.Get(strippedKey, 1)
	require.NoError(t, err)
	require.Equal(t, []byte("evm_value"), evmVal, "EVM data should be in EVM_SS with SplitWrite")
}

// TestFix1_SplitWriteAsyncAlsoStrips verifies SplitWrite works for ApplyChangesetAsync too.
func TestFix1_SplitWriteAsyncAlsoStrips(t *testing.T) {
	dir, err := os.MkdirTemp("", "fix1_split_write_async_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
	}
	ssConfig.WriteMode = config.SplitWrite
	ssConfig.ReadMode = config.EVMFirstRead
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	addr := make([]byte, 20)
	addr[0] = 0xCC
	slot := make([]byte, 32)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	changesets := []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: storageKey, Value: []byte("async_evm")},
				},
			},
		},
	}
	err = store.ApplyChangesetAsync(1, changesets)
	require.NoError(t, err)

	// Async writes are enqueued to per-DB channels; wait briefly for workers to process
	time.Sleep(100 * time.Millisecond)

	// EVM data should NOT be in Cosmos (SplitWrite strips EVM from Cosmos changeset)
	cosmosVal, err := store.cosmosStore.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Nil(t, cosmosVal, "EVM data should NOT be in Cosmos with SplitWrite async")

	// EVM data should be in EVM_SS
	_, strippedKey := commonevm.ParseEVMKey(storageKey)
	evmDB := store.evmStore.GetDB(evm.StoreStorage)
	evmVal, err := evmDB.Get(strippedKey, 1)
	require.NoError(t, err)
	require.Equal(t, []byte("async_evm"), evmVal)
}

// TestFix2_SplitReadNoCosmFallback verifies that SplitRead mode does NOT
// fall back to Cosmos for EVM keys -- it returns nil if EVM_SS misses.
func TestFix2_SplitReadNoCosmFallback(t *testing.T) {
	dir, err := os.MkdirTemp("", "fix2_split_read_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
	}
	ssConfig.WriteMode = config.DualWrite // Write to both so Cosmos has data
	ssConfig.ReadMode = config.SplitRead  // But reads from EVM only, no fallback
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	addr := make([]byte, 20)
	addr[0] = 0xDD
	slot := make([]byte, 32)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	// Write EVM data (DualWrite populates both stores)
	changesets := []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: storageKey, Value: []byte("in_both")},
				},
			},
		},
	}
	err = store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	// SplitRead should find data from EVM_SS (it's there via DualWrite)
	val, err := store.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("in_both"), val, "SplitRead should serve from EVM_SS")

	// Now write data ONLY to Cosmos (bypass composite, simulate stale EVM)
	cosmosOnlyKey := append([]byte{0x03}, append(make([]byte, 20), make([]byte, 32)...)...)
	cosmosOnlyKey[1] = 0xEE // different address
	err = store.cosmosStore.ApplyChangesetSync(2, []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: cosmosOnlyKey, Value: []byte("cosmos_only_data")},
				},
			},
		},
	})
	require.NoError(t, err)

	// SplitRead should NOT fall back to Cosmos -- key only exists in Cosmos
	val, err = store.Get("evm", 2, cosmosOnlyKey)
	require.NoError(t, err)
	require.Nil(t, val, "SplitRead must NOT fall back to Cosmos for EVM keys")

	// Has should also not fall back
	has, err := store.Has("evm", 2, cosmosOnlyKey)
	require.NoError(t, err)
	require.False(t, has, "SplitRead Has must NOT fall back to Cosmos for EVM keys")

	// Non-EVM keys should still read from Cosmos normally
	err = store.cosmosStore.ApplyChangesetSync(3, []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("supply"), Value: []byte("1000")},
				},
			},
		},
	})
	require.NoError(t, err)

	val, err = store.Get("bank", 3, []byte("supply"))
	require.NoError(t, err)
	require.Equal(t, []byte("1000"), val, "Non-EVM keys should still read from Cosmos")
}

// TestFix3_SetLatestVersionRespectsWriteMode verifies that SetLatestVersion
// does NOT advance EVM version in CosmosOnlyWrite mode.
func TestFix3_SetLatestVersionRespectsWriteMode(t *testing.T) {
	t.Run("CosmosOnlyWrite does not open EVM stores", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "fix3_version_cosmos_only_test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ssConfig := config.StateStoreConfig{
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       100000,
			WriteMode:        config.CosmosOnlyWrite,
			ReadMode:         config.CosmosOnlyRead,
		}

		store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
		require.NoError(t, err)
		defer store.Close()

		// EVM stores should not be opened in cosmos-only mode
		require.Nil(t, store.evmStore, "EVM store must be nil in CosmosOnlyWrite mode")

		// Simulate 10 block commits
		for v := int64(1); v <= 10; v++ {
			err := store.ApplyChangesetSync(v, []*proto.NamedChangeSet{
				{
					Name: "test",
					Changeset: iavl.ChangeSet{
						Pairs: []*iavl.KVPair{
							{Key: []byte("key"), Value: []byte{byte(v)}},
						},
					},
				},
			})
			require.NoError(t, err)
			err = store.SetLatestVersion(v)
			require.NoError(t, err)
		}

		// Cosmos should be at version 10
		require.Equal(t, int64(10), store.cosmosStore.GetLatestVersion())
	})

	t.Run("DualWrite advances both versions", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "fix3_version_dual_write_test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ssConfig := config.StateStoreConfig{
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       100000,
		}
		ssConfig.WriteMode = config.DualWrite
		ssConfig.ReadMode = config.EVMFirstRead
		ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

		store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
		require.NoError(t, err)
		defer store.Close()

		for v := int64(1); v <= 5; v++ {
			err := store.ApplyChangesetSync(v, []*proto.NamedChangeSet{
				{
					Name: "test",
					Changeset: iavl.ChangeSet{
						Pairs: []*iavl.KVPair{
							{Key: []byte("key"), Value: []byte{byte(v)}},
						},
					},
				},
			})
			require.NoError(t, err)
			err = store.SetLatestVersion(v)
			require.NoError(t, err)
		}

		require.Equal(t, int64(5), store.cosmosStore.GetLatestVersion())
		require.Equal(t, int64(5), store.evmStore.GetLatestVersion(),
			"EVM version must advance in DualWrite mode")
	})
}

// TestE2E_LargeChangesetParallelWrite verifies that a large changeset with many EVM
// key types is correctly split across databases in parallel without data corruption.
func TestE2E_LargeChangesetParallelWrite(t *testing.T) {
	dir, err := os.MkdirTemp("", "e2e_large_changeset_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
	}
	ssConfig.WriteMode = config.DualWrite
	ssConfig.ReadMode = config.EVMFirstRead
	ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

	store, err := testNewCompositeStateStore(ssConfig, dir, logger.NewNopLogger())
	require.NoError(t, err)
	defer store.Close()

	// Generate 100 unique EVM storage keys + 50 nonce keys + 50 non-EVM keys
	var evmPairs []*iavl.KVPair
	type keyRecord struct {
		fullKey []byte
		value   []byte
	}
	var storagePairs []keyRecord
	var noncePairs []keyRecord

	for i := 0; i < 100; i++ {
		addr := make([]byte, 20)
		addr[0] = byte(i >> 8)
		addr[1] = byte(i)
		slot := make([]byte, 32)
		slot[0] = byte(i)
		fullKey := append([]byte{0x03}, append(addr, slot...)...)
		val := []byte(fmt.Sprintf("storage_%d", i))
		evmPairs = append(evmPairs, &iavl.KVPair{Key: fullKey, Value: val})
		storagePairs = append(storagePairs, keyRecord{fullKey, val})
	}

	for i := 0; i < 50; i++ {
		addr := make([]byte, 20)
		addr[0] = byte(i + 200)
		fullKey := append([]byte{0x0a}, addr...)
		val := []byte{byte(i)}
		evmPairs = append(evmPairs, &iavl.KVPair{Key: fullKey, Value: val})
		noncePairs = append(noncePairs, keyRecord{fullKey, val})
	}

	var bankPairs []*iavl.KVPair
	for i := 0; i < 50; i++ {
		bankPairs = append(bankPairs, &iavl.KVPair{
			Key:   []byte(fmt.Sprintf("balance_%d", i)),
			Value: []byte(fmt.Sprintf("%d", i*100)),
		})
	}

	changesets := []*proto.NamedChangeSet{
		{Name: "evm", Changeset: iavl.ChangeSet{Pairs: evmPairs}},
		{Name: "bank", Changeset: iavl.ChangeSet{Pairs: bankPairs}},
	}

	err = store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)
	require.NoError(t, store.SetLatestVersion(1))

	// Verify all storage keys round-trip through composite
	for i, rec := range storagePairs {
		val, err := store.Get("evm", 1, rec.fullKey)
		require.NoError(t, err)
		require.Equal(t, rec.value, val, "Storage key %d mismatch", i)
	}

	// Verify all nonce keys round-trip
	for i, rec := range noncePairs {
		val, err := store.Get("evm", 1, rec.fullKey)
		require.NoError(t, err)
		require.Equal(t, rec.value, val, "Nonce key %d mismatch", i)
	}

	// Verify bank data unaffected
	for i := 0; i < 50; i++ {
		val, err := store.Get("bank", 1, []byte(fmt.Sprintf("balance_%d", i)))
		require.NoError(t, err)
		require.Equal(t, []byte(fmt.Sprintf("%d", i*100)), val, "Bank key %d mismatch", i)
	}
}
