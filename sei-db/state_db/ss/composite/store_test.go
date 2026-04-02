package composite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/stretchr/testify/require"
)

type mockImportStateStore struct {
	importFn func(version int64, ch <-chan types.SnapshotNode) error
}

func (m *mockImportStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	return nil, nil
}

func (m *mockImportStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	return false, nil
}

func (m *mockImportStateStore) Iterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return nil, nil
}

func (m *mockImportStateStore) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return nil, nil
}

func (m *mockImportStateStore) RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error) {
	return false, nil
}

func (m *mockImportStateStore) GetLatestVersion() int64 {
	return 0
}

func (m *mockImportStateStore) SetLatestVersion(version int64) error {
	return nil
}

func (m *mockImportStateStore) GetEarliestVersion() int64 {
	return 0
}

func (m *mockImportStateStore) SetEarliestVersion(version int64, ignoreVersion bool) error {
	return nil
}

func (m *mockImportStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	return nil
}

func (m *mockImportStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	return nil
}

func (m *mockImportStateStore) Prune(version int64) error {
	return nil
}

func (m *mockImportStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	if m.importFn != nil {
		return m.importFn(version, ch)
	}
	return nil
}

func (m *mockImportStateStore) Close() error {
	return nil
}

func setupTestStores(t *testing.T) (*CompositeStateStore, string, func()) {
	dir, err := os.MkdirTemp("", "composite_store_test")
	require.NoError(t, err)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		WriteMode:        config.DualWrite,
		ReadMode:         config.EVMFirstRead,
		EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
	}

	compositeStore, err := NewCompositeStateStore(ssConfig, dir)
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
		changesets := []*proto.NamedChangeSet{
			{
				Name: "bank",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: []byte("balance1"), Value: []byte("100")},
					},
				},
			},
		}
		err := store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

		val, err := store.Get("bank", 1, []byte("balance1"))
		require.NoError(t, err)
		require.Equal(t, []byte("100"), val)

		has, err := store.Has("bank", 1, []byte("balance1"))
		require.NoError(t, err)
		require.True(t, has)

		val, err = store.Get("bank", 1, []byte("nonexistent"))
		require.NoError(t, err)
		require.Nil(t, val)
	})

	t.Run("Get EVM key falls back to Cosmos", func(t *testing.T) {
		addr := make([]byte, 20)
		slot := make([]byte, 32)
		storageKey := append([]byte{0x03}, append(addr, slot...)...)

		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: storageKey, Value: []byte("storage_value")},
					},
				},
			},
		}
		err := store.ApplyChangesetSync(2, changesets)
		require.NoError(t, err)

		val, err := store.Get("evm", 2, storageKey)
		require.NoError(t, err)
		require.Equal(t, []byte("storage_value"), val)
	})
}

func TestCompositeStateStoreIterator(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	changesets := []*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
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

	require.Equal(t, int64(0), store.GetLatestVersion())

	changesets := []*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
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

	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	changesets := []*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
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

	changesets := []*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: []byte("exists"), Value: []byte("value")},
				},
			},
		},
	}
	err := store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	has, err := store.Has("test", 1, []byte("exists"))
	require.NoError(t, err)
	require.True(t, has)

	has, err = store.Has("test", 1, []byte("nonexistent"))
	require.NoError(t, err)
	require.False(t, has)

	has, err = store.Has("test", 0, []byte("exists"))
	require.NoError(t, err)
	require.False(t, has)
}

func TestCompositeStateStoreDualWrite(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	addr := make([]byte, 20)
	addr[0] = 0x01
	slot := make([]byte, 32)
	slot[0] = 0x01
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	t.Run("EVM data dual-written", func(t *testing.T) {
		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: storageKey, Value: []byte("storage_value")},
					},
				},
			},
		}
		err := store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

		val, err := store.Get("evm", 1, storageKey)
		require.NoError(t, err)
		require.Equal(t, []byte("storage_value"), val)

		// Also verify EVM store has the data directly
		if store.evmStore != nil {
			evmVal, err := store.evmStore.Get("evm", 1, storageKey)
			require.NoError(t, err)
			require.Equal(t, []byte("storage_value"), evmVal)
		}
	})

	t.Run("Non-EVM data only to Cosmos", func(t *testing.T) {
		changesets := []*proto.NamedChangeSet{
			{
				Name: "bank",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: []byte("balance"), Value: []byte("100")},
					},
				},
			},
		}
		err := store.ApplyChangesetSync(2, changesets)
		require.NoError(t, err)

		val, err := store.Get("bank", 2, []byte("balance"))
		require.NoError(t, err)
		require.Equal(t, []byte("100"), val)
	})
}

func TestCompositeStateStoreMixedChangeset(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	addr := make([]byte, 20)
	addr[0] = 0x02

	nonceKey := append([]byte{0x0a}, addr...)
	codeKey := append([]byte{0x07}, addr...)

	changesets := []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: []byte("balance"), Value: []byte("500")},
				},
			},
		},
		{
			Name: "evm",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: nonceKey, Value: []byte{0x01}},
					{Key: codeKey, Value: []byte{0x60, 0x80}},
				},
			},
		},
		{
			Name: "staking",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: []byte("validator"), Value: []byte("active")},
				},
			},
		},
	}

	err := store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

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

	changesets := []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: storageKey, Value: []byte("value")},
				},
			},
		},
	}
	err := store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	changesets = []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: storageKey, Delete: true},
				},
			},
		},
	}
	err = store.ApplyChangesetSync(2, changesets)
	require.NoError(t, err)

	val, err := store.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("value"), val)

	val, err = store.Get("evm", 2, storageKey)
	require.NoError(t, err)
	require.Nil(t, val)
}

func TestBug1Fix_WriteModeControlsEVMWrites(t *testing.T) {
	addr := make([]byte, 20)
	addr[0] = 0xAA
	slot := make([]byte, 32)
	slot[0] = 0xBB
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

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

		store, err := NewCompositeStateStore(ssConfig, dir)
		require.NoError(t, err)
		defer store.Close()

		require.Nil(t, store.evmStore, "EVM store should be nil in cosmos-only mode")

		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: storageKey, Value: []byte("cosmos_only")},
					},
				},
			},
		}
		err = store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

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
			WriteMode:        config.DualWrite,
			ReadMode:         config.EVMFirstRead,
			EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
		}

		store, err := NewCompositeStateStore(ssConfig, dir)
		require.NoError(t, err)
		defer store.Close()

		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: storageKey, Value: []byte("in_both_stores")},
					},
				},
			},
		}
		err = store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

		cosmosVal, err := store.cosmosStore.Get("evm", 1, storageKey)
		require.NoError(t, err)
		require.Equal(t, []byte("in_both_stores"), cosmosVal)

		evmVal, err := store.evmStore.Get("evm", 1, storageKey)
		require.NoError(t, err)
		require.Equal(t, []byte("in_both_stores"), evmVal)
	})
}

func TestBug1Fix_ReadModeControlsEVMReads(t *testing.T) {
	addr := make([]byte, 20)
	addr[0] = 0xCC
	slot := make([]byte, 32)
	slot[0] = 0xDD
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	t.Run("CosmosOnlyRead never checks EVM even if EVM has data", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "bug1_read_cosmos_only_test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ssConfig := config.StateStoreConfig{
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       100000,
			WriteMode:        config.DualWrite,
			ReadMode:         config.CosmosOnlyRead,
			EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
		}

		store, err := NewCompositeStateStore(ssConfig, dir)
		require.NoError(t, err)
		defer store.Close()

		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: storageKey, Value: []byte("cosmos_value")},
					},
				},
			},
		}
		err = store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

		val, err := store.Get("evm", 1, storageKey)
		require.NoError(t, err)
		require.Equal(t, []byte("cosmos_value"), val, "CosmosOnlyRead should read from cosmos")
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
			ReadMode:         config.EVMFirstRead,
			EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
		}

		store, err := NewCompositeStateStore(ssConfig, dir)
		require.NoError(t, err)
		defer store.Close()

		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: storageKey, Value: []byte("dual_written")},
					},
				},
			},
		}
		err = store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

		val, err := store.Get("evm", 1, storageKey)
		require.NoError(t, err)
		require.Equal(t, []byte("dual_written"), val)

		// Verify via EVM store directly
		evmVal, err := store.evmStore.Get("evm", 1, storageKey)
		require.NoError(t, err)
		require.Equal(t, []byte("dual_written"), evmVal)

		has, err := store.Has("evm", 1, storageKey)
		require.NoError(t, err)
		require.True(t, has)
	})
}

func TestCodeSizeGoesToLegacy(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

	store.config.ReadMode = config.EVMFirstRead

	addr := make([]byte, 20)
	addr[0] = 0x42
	addr[19] = 0xFF
	codeSizeKey := append([]byte{0x09}, addr...)
	codeSizeValue := []byte{0x00, 0x00, 0x10, 0x00}

	changesets := []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: codeSizeKey, Value: codeSizeValue},
				},
			},
		},
	}
	err := store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	compositeVal, err := store.Get("evm", 1, codeSizeKey)
	require.NoError(t, err)
	require.Equal(t, codeSizeValue, compositeVal)
}

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

	nonceKey := append([]byte{0x0a}, addr...)
	codeHashKey := append([]byte{0x08}, addr...)
	codeKey := append([]byte{0x07}, addr...)
	codeSizeKey := append([]byte{0x09}, addr...)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)
	legacyKey := append([]byte{0x01}, addr...)

	changesets := []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
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

	tests := []struct {
		name    string
		fullKey []byte
		value   []byte
	}{
		{"Nonce", nonceKey, []byte{0x05}},
		{"CodeHash", codeHashKey, []byte("hash_abc")},
		{"Code", codeKey, []byte{0x60, 0x80, 0x60, 0x40}},
		{"CodeSize (legacy)", codeSizeKey, []byte{0x00, 0x04}},
		{"Storage", storageKey, []byte("storage_val")},
		{"Legacy", legacyKey, []byte("sei1abc")},
	}

	for _, tc := range tests {
		t.Run(tc.name+" via EVM store", func(t *testing.T) {
			val, err := store.evmStore.Get("evm", 1, tc.fullKey)
			require.NoError(t, err)
			require.Equal(t, tc.value, val)
		})
		t.Run(tc.name+" via composite Get", func(t *testing.T) {
			val, err := store.Get("evm", 1, tc.fullKey)
			require.NoError(t, err)
			require.Equal(t, tc.value, val)
		})
	}
}

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
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: storageKey, Value: []byte("async_value")},
				},
			},
		},
	}

	err := store.ApplyChangesetAsync(1, changesets)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	evmVal, err := store.evmStore.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("async_value"), evmVal)
}

func TestCompositeStateStorePrunesBothStores(t *testing.T) {
	dir, err := os.MkdirTemp("", "composite_prune_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       5,
		WriteMode:        config.DualWrite,
		EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
	}

	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	addr := make([]byte, 20)
	addr[0] = 0x01
	slot := make([]byte, 32)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	for v := int64(1); v <= 10; v++ {
		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
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

	pruneVersion := int64(5)
	err = store.Prune(pruneVersion)
	require.NoError(t, err)

	val, err := store.evmStore.Get("evm", 6, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte{6}, val)

	val, err = store.evmStore.Get("evm", 10, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte{10}, val)
}

func TestE2E_AllEVMDBsReadableViaComposite(t *testing.T) {
	dir, err := os.MkdirTemp("", "e2e_all_dbs_test")
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

	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	addr := make([]byte, 20)
	for i := range addr {
		addr[i] = byte(i + 0x10)
	}
	slot := make([]byte, 32)
	for i := range slot {
		slot[i] = byte(i + 0xA0)
	}

	tests := []struct {
		name    string
		fullKey []byte
		value   []byte
	}{
		{"Nonce", append([]byte{0x0a}, addr...), []byte{0x00, 0x00, 0x00, 0x2A}},
		{"CodeHash", append([]byte{0x08}, addr...), []byte("deadbeef01234567890abcdef1234567")},
		{"Code", append([]byte{0x07}, addr...), []byte{0x60, 0x80, 0x60, 0x40, 0x52, 0x34, 0x80, 0x15}},
		{"CodeSize (legacy)", append([]byte{0x09}, addr...), []byte{0x00, 0x00, 0x20, 0x00}},
		{"Storage", append([]byte{0x03}, append(addr, slot...)...), []byte("storage_value_at_slot")},
		{"Legacy (EVMToSeiAddr)", append([]byte{0x01}, addr...), []byte("sei1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu")},
	}

	var pairs []*proto.KVPair
	for _, tc := range tests {
		pairs = append(pairs, &proto.KVPair{Key: tc.fullKey, Value: tc.value})
	}
	changesets := []*proto.NamedChangeSet{
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: pairs}},
	}
	err = store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)
	err = store.SetLatestVersion(1)
	require.NoError(t, err)

	for _, tc := range tests {
		t.Run(tc.name+"_EVM_direct", func(t *testing.T) {
			val, err := store.evmStore.Get("evm", 1, tc.fullKey)
			require.NoError(t, err)
			require.Equal(t, tc.value, val)
		})
		t.Run(tc.name+"_composite_Get", func(t *testing.T) {
			val, err := store.Get("evm", 1, tc.fullKey)
			require.NoError(t, err)
			require.Equal(t, tc.value, val)
		})
		t.Run(tc.name+"_composite_Has", func(t *testing.T) {
			has, err := store.Has("evm", 1, tc.fullKey)
			require.NoError(t, err)
			require.True(t, has)
		})
	}
}

func TestE2E_MVCCConsistencyAcrossBothStores(t *testing.T) {
	dir, err := os.MkdirTemp("", "e2e_mvcc_test")
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

	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	addr := make([]byte, 20)
	addr[0] = 0xDE
	addr[19] = 0xAD
	slot := make([]byte, 32)
	slot[0] = 0xBE
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	for v := int64(1); v <= 5; v++ {
		val := []byte(fmt.Sprintf("value_at_v%d", v))
		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
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

	for v := int64(1); v <= 5; v++ {
		expected := []byte(fmt.Sprintf("value_at_v%d", v))

		t.Run(fmt.Sprintf("composite_Get_v%d", v), func(t *testing.T) {
			val, err := store.Get("evm", v, storageKey)
			require.NoError(t, err)
			require.Equal(t, expected, val)
		})
		t.Run(fmt.Sprintf("cosmos_direct_v%d", v), func(t *testing.T) {
			val, err := store.cosmosStore.Get("evm", v, storageKey)
			require.NoError(t, err)
			require.Equal(t, expected, val)
		})
		t.Run(fmt.Sprintf("evm_direct_v%d", v), func(t *testing.T) {
			val, err := store.evmStore.Get("evm", v, storageKey)
			require.NoError(t, err)
			require.Equal(t, expected, val)
		})
	}

	require.Equal(t, int64(5), store.GetLatestVersion())
	require.Equal(t, int64(5), store.cosmosStore.GetLatestVersion())
	require.Equal(t, int64(5), store.evmStore.GetLatestVersion())
}

func TestE2E_NonEVMModulesUnaffectedByDualWrite(t *testing.T) {
	dir, err := os.MkdirTemp("", "e2e_non_evm_test")
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

	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	addr := make([]byte, 20)
	slot := make([]byte, 32)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	changesets := []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: []byte("supply/usei"), Value: []byte("1000000000")},
					{Key: []byte("balances/sei1abc/usei"), Value: []byte("500")},
				},
			},
		},
		{
			Name: "evm",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: storageKey, Value: []byte("evm_slot_data")},
				},
			},
		},
		{
			Name: "staking",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: []byte("validators/sei1val"), Value: []byte("bonded")},
				},
			},
		},
	}

	err = store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)
	err = store.SetLatestVersion(1)
	require.NoError(t, err)

	val, err := store.Get("bank", 1, []byte("supply/usei"))
	require.NoError(t, err)
	require.Equal(t, []byte("1000000000"), val)

	val, err = store.Get("bank", 1, []byte("balances/sei1abc/usei"))
	require.NoError(t, err)
	require.Equal(t, []byte("500"), val)

	val, err = store.Get("staking", 1, []byte("validators/sei1val"))
	require.NoError(t, err)
	require.Equal(t, []byte("bonded"), val)

	val, err = store.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("evm_slot_data"), val)

	has, err := store.Has("bank", 1, []byte("supply/usei"))
	require.NoError(t, err)
	require.True(t, has)

	val, err = store.Get("auth", 1, []byte("some_key"))
	require.NoError(t, err)
	require.Nil(t, val)

	iter, err := store.Iterator("bank", 1, nil, nil)
	require.NoError(t, err)
	defer iter.Close()
	count := 0
	for ; iter.Valid(); iter.Next() {
		count++
	}
	require.Equal(t, 2, count)
}

func TestE2E_VersionConsistencyAfterSetLatestVersion(t *testing.T) {
	dir, err := os.MkdirTemp("", "e2e_version_sync_test")
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

	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	for v := int64(1); v <= 10; v++ {
		changesets := []*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: proto.ChangeSet{
					Pairs: []*proto.KVPair{
						{Key: []byte("key"), Value: []byte{byte(v)}},
					},
				},
			},
		}
		err := store.ApplyChangesetSync(v, changesets)
		require.NoError(t, err)
		err = store.SetLatestVersion(v)
		require.NoError(t, err)

		require.Equal(t, v, store.GetLatestVersion())
		require.Equal(t, v, store.cosmosStore.GetLatestVersion())
		require.Equal(t, v, store.evmStore.GetLatestVersion())
	}
}

func TestE2E_DeleteTombstonePropagatedToBothStores(t *testing.T) {
	dir, err := os.MkdirTemp("", "e2e_delete_test")
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

	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	addr := make([]byte, 20)
	addr[0] = 0xFF
	slot := make([]byte, 32)
	slot[0] = 0xEE
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	err = store.ApplyChangesetSync(1, []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: storageKey, Value: []byte("alive")},
				},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, store.SetLatestVersion(1))

	err = store.ApplyChangesetSync(2, []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: storageKey, Delete: true},
				},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, store.SetLatestVersion(2))

	val, err := store.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("alive"), val)

	cosmosVal, err := store.cosmosStore.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("alive"), cosmosVal)

	evmVal, err := store.evmStore.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("alive"), evmVal)

	val, err = store.Get("evm", 2, storageKey)
	require.NoError(t, err)
	require.Nil(t, val)

	cosmosVal, err = store.cosmosStore.Get("evm", 2, storageKey)
	require.NoError(t, err)
	require.Nil(t, cosmosVal)

	evmVal, err = store.evmStore.Get("evm", 2, storageKey)
	require.NoError(t, err)
	require.Nil(t, evmVal)

	err = store.ApplyChangesetSync(3, []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: storageKey, Value: []byte("resurrected")},
				},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, store.SetLatestVersion(3))

	val, err = store.Get("evm", 3, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("resurrected"), val)
}

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

		store, err := NewCompositeStateStore(ssConfig, dir)
		require.NoError(t, err)
		defer store.Close()

		require.NotNil(t, store.evmStore)
		require.NotNil(t, store.cosmosStore)
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

		store, err := NewCompositeStateStore(ssConfig, dir)
		require.NoError(t, err)
		defer store.Close()

		require.Nil(t, store.evmStore)
		require.NotNil(t, store.cosmosStore)
	})
}

func TestFix1_SplitWriteStripsEVMFromCosmos(t *testing.T) {
	dir, err := os.MkdirTemp("", "fix1_split_write_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		WriteMode:        config.SplitWrite,
		ReadMode:         config.SplitRead,
		EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
	}

	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	addr := make([]byte, 20)
	addr[0] = 0xAA
	slot := make([]byte, 32)
	slot[0] = 0xBB
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	changesets := []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: []byte("balance"), Value: []byte("100")},
				},
			},
		},
		{
			Name: "evm",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: storageKey, Value: []byte("evm_value")},
				},
			},
		},
	}
	err = store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	bankVal, err := store.cosmosStore.Get("bank", 1, []byte("balance"))
	require.NoError(t, err)
	require.Equal(t, []byte("100"), bankVal)

	cosmosEVMVal, err := store.cosmosStore.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Nil(t, cosmosEVMVal, "EVM data should NOT be in Cosmos with SplitWrite")

	evmVal, err := store.evmStore.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("evm_value"), evmVal)
}

func TestFix1_SplitWriteAsyncAlsoStrips(t *testing.T) {
	dir, err := os.MkdirTemp("", "fix1_split_write_async_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		WriteMode:        config.SplitWrite,
		ReadMode:         config.EVMFirstRead,
		EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
	}

	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	addr := make([]byte, 20)
	addr[0] = 0xCC
	slot := make([]byte, 32)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	changesets := []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: storageKey, Value: []byte("async_evm")},
				},
			},
		},
	}
	err = store.ApplyChangesetAsync(1, changesets)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	cosmosVal, err := store.cosmosStore.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Nil(t, cosmosVal, "EVM data should NOT be in Cosmos with SplitWrite async")

	evmVal, err := store.evmStore.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("async_evm"), evmVal)
}

func TestFix2_SplitReadNoCosmFallback(t *testing.T) {
	dir, err := os.MkdirTemp("", "fix2_split_read_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		WriteMode:        config.DualWrite,
		ReadMode:         config.SplitRead,
		EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
	}

	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	addr := make([]byte, 20)
	addr[0] = 0xDD
	slot := make([]byte, 32)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	changesets := []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: storageKey, Value: []byte("in_both")},
				},
			},
		},
	}
	err = store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	val, err := store.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("in_both"), val)

	cosmosOnlyKey := append([]byte{0x03}, append(make([]byte, 20), make([]byte, 32)...)...)
	cosmosOnlyKey[1] = 0xEE
	err = store.cosmosStore.ApplyChangesetSync(2, []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: cosmosOnlyKey, Value: []byte("cosmos_only_data")},
				},
			},
		},
	})
	require.NoError(t, err)

	val, err = store.Get("evm", 2, cosmosOnlyKey)
	require.NoError(t, err)
	require.Nil(t, val, "SplitRead must NOT fall back to Cosmos for EVM keys")

	has, err := store.Has("evm", 2, cosmosOnlyKey)
	require.NoError(t, err)
	require.False(t, has)

	err = store.cosmosStore.ApplyChangesetSync(3, []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: proto.ChangeSet{
				Pairs: []*proto.KVPair{
					{Key: []byte("supply"), Value: []byte("1000")},
				},
			},
		},
	})
	require.NoError(t, err)

	val, err = store.Get("bank", 3, []byte("supply"))
	require.NoError(t, err)
	require.Equal(t, []byte("1000"), val)
}

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

		store, err := NewCompositeStateStore(ssConfig, dir)
		require.NoError(t, err)
		defer store.Close()

		require.Nil(t, store.evmStore)

		for v := int64(1); v <= 10; v++ {
			err := store.ApplyChangesetSync(v, []*proto.NamedChangeSet{
				{
					Name: "test",
					Changeset: proto.ChangeSet{
						Pairs: []*proto.KVPair{
							{Key: []byte("key"), Value: []byte{byte(v)}},
						},
					},
				},
			})
			require.NoError(t, err)
			err = store.SetLatestVersion(v)
			require.NoError(t, err)
		}

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
			WriteMode:        config.DualWrite,
			ReadMode:         config.EVMFirstRead,
			EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
		}

		store, err := NewCompositeStateStore(ssConfig, dir)
		require.NoError(t, err)
		defer store.Close()

		for v := int64(1); v <= 5; v++ {
			err := store.ApplyChangesetSync(v, []*proto.NamedChangeSet{
				{
					Name: "test",
					Changeset: proto.ChangeSet{
						Pairs: []*proto.KVPair{
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
		require.Equal(t, int64(5), store.evmStore.GetLatestVersion())
	})
}

// setupImportTestStore creates a CompositeStateStore with the given write mode for import tests.
func setupImportTestStore(t *testing.T, writeMode config.WriteMode) (*CompositeStateStore, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "ss_import_test")
	require.NoError(t, err)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       0,
		ImportNumWorkers: 1,
		WriteMode:        writeMode,
		ReadMode:         config.EVMFirstRead,
		EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
	}

	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)

	return store, func() {
		store.Close()
		os.RemoveAll(dir)
	}
}

func feedNodes(ch chan<- types.SnapshotNode, nodes []types.SnapshotNode) {
	for _, n := range nodes {
		ch <- n
	}
	close(ch)
}

func TestImport_OnlyEvmModule(t *testing.T) {
	for _, mode := range []config.WriteMode{config.DualWrite, config.SplitWrite, config.CosmosOnlyWrite} {
		t.Run("WriteMode="+string(mode), func(t *testing.T) {
			store, cleanup := setupImportTestStore(t, mode)
			defer cleanup()

			ch := make(chan types.SnapshotNode, 10)
			nodes := []types.SnapshotNode{
				{StoreKey: "bank", Key: []byte("supply"), Value: []byte("1000")},
				{StoreKey: commonevm.EVMStoreKey, Key: []byte("evm_key_1"), Value: []byte("val_1")},
				{StoreKey: commonevm.EVMStoreKey, Key: []byte("evm_key_2"), Value: []byte("val_2")},
			}
			go feedNodes(ch, nodes)

			err := store.Import(1, ch)
			require.NoError(t, err)

			bankVal, err := store.cosmosStore.Get("bank", 1, []byte("supply"))
			require.NoError(t, err)
			require.Equal(t, []byte("1000"), bankVal)

			cosmosEVM1, err := store.cosmosStore.Get(evm.EVMStoreKey, 1, []byte("evm_key_1"))
			require.NoError(t, err)

			if mode == config.SplitWrite {
				require.Nil(t, cosmosEVM1, "SplitWrite should not store evm data in cosmos")
			} else {
				require.Equal(t, []byte("val_1"), cosmosEVM1)
			}

			if store.evmStore != nil && mode != config.CosmosOnlyWrite {
				evmVal, err := store.evmStore.Get(evm.EVMStoreKey, 1, []byte("evm_key_1"))
				require.NoError(t, err)
				require.Equal(t, []byte("val_1"), evmVal)

				evmVal2, err := store.evmStore.Get(evm.EVMStoreKey, 1, []byte("evm_key_2"))
				require.NoError(t, err)
				require.Equal(t, []byte("val_2"), evmVal2)
			}
		})
	}
}

func TestImport_OnlyEvmFlatkvModule(t *testing.T) {
	for _, mode := range []config.WriteMode{config.DualWrite, config.SplitWrite, config.CosmosOnlyWrite} {
		t.Run("WriteMode="+string(mode), func(t *testing.T) {
			store, cleanup := setupImportTestStore(t, mode)
			defer cleanup()

			ch := make(chan types.SnapshotNode, 10)
			nodes := []types.SnapshotNode{
				{StoreKey: "bank", Key: []byte("supply"), Value: []byte("2000")},
				{StoreKey: commonevm.EVMFlatKVStoreKey, Key: []byte("flatkv_key_1"), Value: []byte("fv_1")},
				{StoreKey: commonevm.EVMFlatKVStoreKey, Key: []byte("flatkv_key_2"), Value: []byte("fv_2")},
			}
			go feedNodes(ch, nodes)

			err := store.Import(1, ch)
			require.NoError(t, err)

			bankVal, err := store.cosmosStore.Get("bank", 1, []byte("supply"))
			require.NoError(t, err)
			require.Equal(t, []byte("2000"), bankVal)

			cosmosEVM1, err := store.cosmosStore.Get(evm.EVMStoreKey, 1, []byte("flatkv_key_1"))
			require.NoError(t, err)

			if mode == config.SplitWrite {
				require.Nil(t, cosmosEVM1, "SplitWrite should not store evm data in cosmos")
			} else {
				require.Equal(t, []byte("fv_1"), cosmosEVM1, "evm_flatkv should be normalized to evm")
			}

			if store.evmStore != nil && mode != config.CosmosOnlyWrite {
				evmVal, err := store.evmStore.Get(evm.EVMStoreKey, 1, []byte("flatkv_key_1"))
				require.NoError(t, err)
				require.Equal(t, []byte("fv_1"), evmVal)

				evmVal2, err := store.evmStore.Get(evm.EVMStoreKey, 1, []byte("flatkv_key_2"))
				require.NoError(t, err)
				require.Equal(t, []byte("fv_2"), evmVal2)
			}
		})
	}
}

func TestImport_BothEvmAndEvmFlatkv(t *testing.T) {
	for _, mode := range []config.WriteMode{config.DualWrite, config.SplitWrite} {
		t.Run("WriteMode="+string(mode), func(t *testing.T) {
			store, cleanup := setupImportTestStore(t, mode)
			defer cleanup()

			ch := make(chan types.SnapshotNode, 20)
			nodes := []types.SnapshotNode{
				{StoreKey: "bank", Key: []byte("supply"), Value: []byte("3000")},
				// Legacy evm module data
				{StoreKey: commonevm.EVMStoreKey, Key: []byte("shared_key"), Value: []byte("from_evm")},
				{StoreKey: commonevm.EVMStoreKey, Key: []byte("evm_only_key"), Value: []byte("evm_only")},
				// evm_flatkv data arriving later — should override shared_key and add new keys
				{StoreKey: commonevm.EVMFlatKVStoreKey, Key: []byte("shared_key"), Value: []byte("from_flatkv")},
				{StoreKey: commonevm.EVMFlatKVStoreKey, Key: []byte("flatkv_only_key"), Value: []byte("flatkv_only")},
			}
			go feedNodes(ch, nodes)

			err := store.Import(1, ch)
			require.NoError(t, err)

			// bank data should be in cosmos
			bankVal, err := store.cosmosStore.Get("bank", 1, []byte("supply"))
			require.NoError(t, err)
			require.Equal(t, []byte("3000"), bankVal)

			// EVM store should have all keys: evm_only, shared (overridden by flatkv), flatkv_only
			require.NotNil(t, store.evmStore)
			evmOnlyVal, err := store.evmStore.Get(evm.EVMStoreKey, 1, []byte("evm_only_key"))
			require.NoError(t, err)
			require.Equal(t, []byte("evm_only"), evmOnlyVal)

			sharedVal, err := store.evmStore.Get(evm.EVMStoreKey, 1, []byte("shared_key"))
			require.NoError(t, err)
			require.Equal(t, []byte("from_flatkv"), sharedVal, "flatkv value should override evm value for shared key")

			flatkvOnlyVal, err := store.evmStore.Get(evm.EVMStoreKey, 1, []byte("flatkv_only_key"))
			require.NoError(t, err)
			require.Equal(t, []byte("flatkv_only"), flatkvOnlyVal)

			if mode == config.DualWrite {
				cosmosShared, err := store.cosmosStore.Get(evm.EVMStoreKey, 1, []byte("shared_key"))
				require.NoError(t, err)
				require.Equal(t, []byte("from_flatkv"), cosmosShared, "cosmos should also see the flatkv override in DualWrite")
			}
		})
	}
}

func TestImport_CosmosOnlyWrite_NormalizesEvmFlatkv(t *testing.T) {
	store, cleanup := setupImportTestStore(t, config.CosmosOnlyWrite)
	defer cleanup()

	ch := make(chan types.SnapshotNode, 10)
	nodes := []types.SnapshotNode{
		{StoreKey: "bank", Key: []byte("supply"), Value: []byte("5000")},
		{StoreKey: commonevm.EVMFlatKVStoreKey, Key: []byte("fk_1"), Value: []byte("fv_1")},
		{StoreKey: commonevm.EVMStoreKey, Key: []byte("ek_1"), Value: []byte("ev_1")},
	}
	go feedNodes(ch, nodes)

	err := store.Import(1, ch)
	require.NoError(t, err)

	bankVal, err := store.cosmosStore.Get("bank", 1, []byte("supply"))
	require.NoError(t, err)
	require.Equal(t, []byte("5000"), bankVal)

	// evm_flatkv normalized to evm — both should land in cosmos store
	fv, err := store.cosmosStore.Get(evm.EVMStoreKey, 1, []byte("fk_1"))
	require.NoError(t, err)
	require.Equal(t, []byte("fv_1"), fv)

	ev, err := store.cosmosStore.Get(evm.EVMStoreKey, 1, []byte("ek_1"))
	require.NoError(t, err)
	require.Equal(t, []byte("ev_1"), ev)
}

func TestImport_NonEvmModulesUnaffected(t *testing.T) {
	store, cleanup := setupImportTestStore(t, config.DualWrite)
	defer cleanup()

	ch := make(chan types.SnapshotNode, 10)
	nodes := []types.SnapshotNode{
		{StoreKey: "bank", Key: []byte("supply"), Value: []byte("9999")},
		{StoreKey: "staking", Key: []byte("validator"), Value: []byte("active")},
		{StoreKey: "auth", Key: []byte("account"), Value: []byte("data")},
	}
	go feedNodes(ch, nodes)

	err := store.Import(1, ch)
	require.NoError(t, err)

	for _, tc := range []struct {
		store, key string
		value      []byte
	}{
		{"bank", "supply", []byte("9999")},
		{"staking", "validator", []byte("active")},
		{"auth", "account", []byte("data")},
	} {
		val, err := store.cosmosStore.Get(tc.store, 1, []byte(tc.key))
		require.NoError(t, err)
		require.Equal(t, tc.value, val, "module %s key %s", tc.store, tc.key)
	}
}

func TestImport_ReturnsEVMErrorWithoutBlocking(t *testing.T) {
	expectedErr := errors.New("evm import failed")
	store := &CompositeStateStore{
		cosmosStore: &mockImportStateStore{
			importFn: func(version int64, ch <-chan types.SnapshotNode) error {
				for range ch {
				}
				return nil
			},
		},
		evmStore: &mockImportStateStore{
			importFn: func(version int64, ch <-chan types.SnapshotNode) error {
				for range ch {
					return expectedErr
				}
				return nil
			},
		},
		config: config.StateStoreConfig{
			WriteMode: config.DualWrite,
		},
	}

	const nodeCount = 256
	ch := make(chan types.SnapshotNode, nodeCount)
	for i := 0; i < nodeCount; i++ {
		ch <- types.SnapshotNode{
			StoreKey: commonevm.EVMStoreKey,
			Key:      []byte{byte(i)},
			Value:    []byte("value"),
		}
	}
	close(ch)

	resultCh := make(chan error, 1)
	go func() {
		resultCh <- store.Import(1, ch)
	}()

	select {
	case err := <-resultCh:
		require.ErrorIs(t, err, expectedErr)
	case <-time.After(2 * time.Second):
		t.Fatal("CompositeStateStore.Import blocked after EVM import error")
	}
}

func TestE2E_LargeChangesetParallelWrite(t *testing.T) {
	dir, err := os.MkdirTemp("", "e2e_large_changeset_test")
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

	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	var evmPairs []*proto.KVPair
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
		evmPairs = append(evmPairs, &proto.KVPair{Key: fullKey, Value: val})
		storagePairs = append(storagePairs, keyRecord{fullKey, val})
	}

	for i := 0; i < 50; i++ {
		addr := make([]byte, 20)
		addr[0] = byte(i + 200)
		fullKey := append([]byte{0x0a}, addr...)
		val := []byte{byte(i)}
		evmPairs = append(evmPairs, &proto.KVPair{Key: fullKey, Value: val})
		noncePairs = append(noncePairs, keyRecord{fullKey, val})
	}

	var bankPairs []*proto.KVPair
	for i := 0; i < 50; i++ {
		bankPairs = append(bankPairs, &proto.KVPair{
			Key:   []byte(fmt.Sprintf("balance_%d", i)),
			Value: []byte(fmt.Sprintf("%d", i*100)),
		})
	}

	changesets := []*proto.NamedChangeSet{
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: evmPairs}},
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: bankPairs}},
	}

	err = store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)
	require.NoError(t, store.SetLatestVersion(1))

	for i, rec := range storagePairs {
		val, err := store.Get("evm", 1, rec.fullKey)
		require.NoError(t, err)
		require.Equal(t, rec.value, val, "Storage key %d mismatch", i)
	}

	for i, rec := range noncePairs {
		val, err := store.Get("evm", 1, rec.fullKey)
		require.NoError(t, err)
		require.Equal(t, rec.value, val, "Nonce key %d mismatch", i)
	}

	for i := 0; i < 50; i++ {
		val, err := store.Get("bank", 1, []byte(fmt.Sprintf("balance_%d", i)))
		require.NoError(t, err)
		require.Equal(t, []byte(fmt.Sprintf("%d", i*100)), val, "Bank key %d mismatch", i)
	}
}
