package composite

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
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

func (m *mockImportStateStore) Iterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	return nil, nil
}

func (m *mockImportStateStore) ReverseIterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
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
		EVMSplit:         true,
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

	t.Run("Get EVM key from EVM store", func(t *testing.T) {
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

func TestCodeSizeGoesToLegacy(t *testing.T) {
	store, _, cleanup := setupTestStores(t)
	defer cleanup()

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

func TestCompositeStateStorePrunesBothStores(t *testing.T) {
	dir, err := os.MkdirTemp("", "composite_prune_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       5,
		EVMSplit:         true,
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
		EVMSplit:         true,
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

func TestE2E_VersionConsistencyAfterSetLatestVersion(t *testing.T) {
	dir, err := os.MkdirTemp("", "e2e_version_sync_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		EVMSplit:         true,
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

func TestE2E_FactoryMethodCreatesCorrectStoreType(t *testing.T) {
	t.Run("EVM enabled creates CompositeStateStore", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "factory_evm_test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ssConfig := config.StateStoreConfig{
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       100000,
			EVMSplit:         true,
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

func TestSplitModeStripsEVMFromCosmos(t *testing.T) {
	dir, err := os.MkdirTemp("", "fix1_evm_split_write_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		EVMSplit:         true,
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
	require.Nil(t, cosmosEVMVal, "EVM data should NOT be in Cosmos under EVMSplit")

	evmVal, err := store.evmStore.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("evm_value"), evmVal)
}

func TestSplitModeAsyncAlsoStripsEVMFromCosmos(t *testing.T) {
	dir, err := os.MkdirTemp("", "fix1_evm_split_write_async_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		EVMSplit:         true,
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
	require.Nil(t, cosmosVal, "EVM data should NOT be in Cosmos under EVMSplit async")

	evmVal, err := store.evmStore.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("async_evm"), evmVal)
}

func TestSplitModeNoCosmosFallback(t *testing.T) {
	dir, err := os.MkdirTemp("", "fix2_evm_split_read_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		EVMSplit:         true,
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
					{Key: storageKey, Value: []byte("in_evm")},
				},
			},
		},
	}
	err = store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	val, err := store.Get("evm", 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("in_evm"), val)

	// Write an EVM key directly to the cosmos backend to simulate stale data.
	// Under EVMSplit, composite Get must NOT fall back to cosmos for EVM keys.
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
	require.Nil(t, val, "EVMSplit must NOT fall back to Cosmos for EVM keys")

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

func TestSetLatestVersionRespectsEVMMode(t *testing.T) {
	t.Run("EVMSplit=false does not open EVM stores", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "fix3_version_no_evm_split_test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ssConfig := config.StateStoreConfig{
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       100000,
			EVMSplit:         false,
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

	t.Run("Split advances both versions", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "fix3_version_split_test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ssConfig := config.StateStoreConfig{
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       100000,
			EVMSplit:         true,
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

// setupImportTestStore creates a CompositeStateStore with the given EVM split flag for import tests.
func setupImportTestStore(t *testing.T, evmSplit bool) (*CompositeStateStore, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "ss_import_test")
	require.NoError(t, err)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       0,
		ImportNumWorkers: 1,
		EVMSplit:         evmSplit,
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
	for _, mode := range []bool{true, false} {
		t.Run(fmt.Sprintf("EVMSplit=%v", mode), func(t *testing.T) {
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

			if store.evmStore != nil && mode {
				// EVM keys go exclusively to EVM store
				evmVal, err := store.evmStore.Get(evm.EVMStoreKey, 1, []byte("evm_key_1"))
				require.NoError(t, err)
				require.Equal(t, []byte("val_1"), evmVal)

				evmVal2, err := store.evmStore.Get(evm.EVMStoreKey, 1, []byte("evm_key_2"))
				require.NoError(t, err)
				require.Equal(t, []byte("val_2"), evmVal2)

				// EVM keys should not be in cosmos store
				cosmosEVM1, err := store.cosmosStore.Get(evm.EVMStoreKey, 1, []byte("evm_key_1"))
				require.NoError(t, err)
				require.Nil(t, cosmosEVM1, "EVM data should not be in cosmos store")
			} else {
				// No EVM store: EVM keys fall through to cosmos
				cosmosEVM1, err := store.cosmosStore.Get(evm.EVMStoreKey, 1, []byte("evm_key_1"))
				require.NoError(t, err)
				require.Equal(t, []byte("val_1"), cosmosEVM1)
			}
		})
	}
}

func TestImport_OnlyEvmFlatkvModule(t *testing.T) {
	addr1 := make([]byte, 20)
	addr1[19] = 0x01
	addr2 := make([]byte, 20)
	addr2[19] = 0x02
	slot := make([]byte, 32)
	slot[31] = 0xAA

	storageVal := [32]byte{0: 0xBB}
	acctVal := vtype.NewAccountData().SetNonce(42).SetCodeHash(&vtype.CodeHash{0: 0xCC}).Serialize()
	storVal := vtype.NewStorageData().SetValue(&storageVal).Serialize()

	physAcct := ktype.EVMPhysicalKey(commonevm.EVMKeyNonce, addr1)
	physStor := ktype.EVMPhysicalKey(commonevm.EVMKeyStorage, append(addr2, slot...))

	nonceKey := commonevm.BuildEVMKey(commonevm.EVMKeyNonce, addr1)
	codeHashKey := commonevm.BuildEVMKey(commonevm.EVMKeyCodeHash, addr1)
	storageKey := commonevm.BuildEVMKey(commonevm.EVMKeyStorage, append(addr2, slot...))

	nonceBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBuf, 42)

	for _, mode := range []bool{true, false} {
		t.Run(fmt.Sprintf("EVMSplit=%v", mode), func(t *testing.T) {
			store, cleanup := setupImportTestStore(t, mode)
			defer cleanup()

			ch := make(chan types.SnapshotNode, 10)
			nodes := []types.SnapshotNode{
				{StoreKey: "bank", Key: []byte("supply"), Value: []byte("2000")},
				{StoreKey: commonevm.FlatKVStoreKey, Key: physAcct, Value: acctVal},
				{StoreKey: commonevm.FlatKVStoreKey, Key: physStor, Value: storVal},
			}
			go feedNodes(ch, nodes)

			err := store.Import(1, ch)
			require.NoError(t, err)

			bankVal, err := store.cosmosStore.Get("bank", 1, []byte("supply"))
			require.NoError(t, err)
			require.Equal(t, []byte("2000"), bankVal)

			if store.evmStore != nil && mode {
				evmNonce, err := store.evmStore.Get(evm.EVMStoreKey, 1, nonceKey)
				require.NoError(t, err)
				require.Equal(t, nonceBuf, evmNonce)

				evmCodeHash, err := store.evmStore.Get(evm.EVMStoreKey, 1, codeHashKey)
				require.NoError(t, err)
				require.Equal(t, vtype.CodeHash{0: 0xCC}, vtype.CodeHash(evmCodeHash))

				evmStor, err := store.evmStore.Get(evm.EVMStoreKey, 1, storageKey)
				require.NoError(t, err)
				require.Equal(t, storageVal[:], evmStor)
			} else {
				cosmosNonce, err := store.cosmosStore.Get(evm.EVMStoreKey, 1, nonceKey)
				require.NoError(t, err)
				require.Equal(t, nonceBuf, cosmosNonce, "converted flatkv data should land in cosmos when no evm store")
			}
		})
	}
}

func TestImport_BothEvmAndEvmFlatkv(t *testing.T) {
	addr := make([]byte, 20)
	addr[19] = 0x03
	slot := make([]byte, 32)
	slot[31] = 0x01
	storageVal := [32]byte{0: 0xDD}

	physStor := ktype.EVMPhysicalKey(commonevm.EVMKeyStorage, append(addr, slot...))
	storVal := vtype.NewStorageData().SetValue(&storageVal).Serialize()
	storageKey := commonevm.BuildEVMKey(commonevm.EVMKeyStorage, append(addr, slot...))

	store, cleanup := setupImportTestStore(t, true)
	defer cleanup()

	ch := make(chan types.SnapshotNode, 20)
	nodes := []types.SnapshotNode{
		{StoreKey: "bank", Key: []byte("supply"), Value: []byte("3000")},
		{StoreKey: commonevm.EVMStoreKey, Key: []byte("evm_only_key"), Value: []byte("evm_only")},
		{StoreKey: commonevm.FlatKVStoreKey, Key: physStor, Value: storVal},
	}
	go feedNodes(ch, nodes)

	err := store.Import(1, ch)
	require.NoError(t, err)

	bankVal, err := store.cosmosStore.Get("bank", 1, []byte("supply"))
	require.NoError(t, err)
	require.Equal(t, []byte("3000"), bankVal)

	require.NotNil(t, store.evmStore)
	evmOnlyVal, err := store.evmStore.Get(evm.EVMStoreKey, 1, []byte("evm_only_key"))
	require.NoError(t, err)
	require.Equal(t, []byte("evm_only"), evmOnlyVal)

	evmStor, err := store.evmStore.Get(evm.EVMStoreKey, 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, storageVal[:], evmStor, "flatkv storage data should be in evm store")
}

func TestImport_EVMSplitDisabled_ConvertsFlatkvToCosmos(t *testing.T) {
	addr := make([]byte, 20)
	addr[19] = 0x05

	physAcct := ktype.EVMPhysicalKey(commonevm.EVMKeyNonce, addr)
	acctVal := vtype.NewAccountData().SetNonce(7).SetCodeHash(&vtype.CodeHash{}).Serialize()

	nonceKey := commonevm.BuildEVMKey(commonevm.EVMKeyNonce, addr)
	nonceBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBuf, 7)

	store, cleanup := setupImportTestStore(t, false)
	defer cleanup()

	ch := make(chan types.SnapshotNode, 10)
	nodes := []types.SnapshotNode{
		{StoreKey: "bank", Key: []byte("supply"), Value: []byte("5000")},
		{StoreKey: commonevm.FlatKVStoreKey, Key: physAcct, Value: acctVal},
		{StoreKey: commonevm.EVMStoreKey, Key: []byte("ek_1"), Value: []byte("ev_1")},
	}
	go feedNodes(ch, nodes)

	err := store.Import(1, ch)
	require.NoError(t, err)

	bankVal, err := store.cosmosStore.Get("bank", 1, []byte("supply"))
	require.NoError(t, err)
	require.Equal(t, []byte("5000"), bankVal)

	cosmosNonce, err := store.cosmosStore.Get(evm.EVMStoreKey, 1, nonceKey)
	require.NoError(t, err)
	require.Equal(t, nonceBuf, cosmosNonce, "converted flatkv nonce should land in cosmos store")

	ev, err := store.cosmosStore.Get(evm.EVMStoreKey, 1, []byte("ek_1"))
	require.NoError(t, err)
	require.Equal(t, []byte("ev_1"), ev)
}

func TestImport_FlatKVLegacyKeysPreserveModule(t *testing.T) {
	addr := make([]byte, 20)
	addr[0] = 0xAA

	evmLegacyInnerKey := append([]byte{0x01}, addr...)
	evmLegacyPhysKey := ktype.ModulePhysicalKey("evm", evmLegacyInnerKey)
	evmLegacyVal := vtype.NewLegacyData().SetValue([]byte("sei1abc")).Serialize()

	bankInnerKey := []byte("balances/addr1")
	bankPhysKey := ktype.ModulePhysicalKey("bank", bankInnerKey)
	bankLegacyVal := vtype.NewLegacyData().SetValue([]byte("1000usei")).Serialize()

	for _, mode := range []bool{true, false} {
		t.Run(fmt.Sprintf("EVMSplit=%v", mode), func(t *testing.T) {
			store, cleanup := setupImportTestStore(t, mode)
			defer cleanup()

			ch := make(chan types.SnapshotNode, 10)
			nodes := []types.SnapshotNode{
				{StoreKey: commonevm.FlatKVStoreKey, Key: evmLegacyPhysKey, Value: evmLegacyVal},
				{StoreKey: commonevm.FlatKVStoreKey, Key: bankPhysKey, Value: bankLegacyVal},
			}
			go feedNodes(ch, nodes)

			err := store.Import(1, ch)
			require.NoError(t, err)

			if store.evmStore != nil && mode {
				evmVal, err := store.evmStore.Get(evm.EVMStoreKey, 1, evmLegacyInnerKey)
				require.NoError(t, err)
				require.Equal(t, []byte("sei1abc"), evmVal, "evm legacy key should land in EVM store")
			}

			bankVal, err := store.cosmosStore.Get("bank", 1, bankInnerKey)
			require.NoError(t, err)
			require.Equal(t, []byte("1000usei"), bankVal, "bank legacy key should land in cosmos under 'bank' module")

			wrongModule, err := store.cosmosStore.Get(evm.EVMStoreKey, 1, bankInnerKey)
			require.NoError(t, err)
			require.Nil(t, wrongModule, "bank legacy key should NOT land under evm store key")
		})
	}
}

func TestImport_NonEvmModulesUnaffected(t *testing.T) {
	store, cleanup := setupImportTestStore(t, true)
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
			EVMSplit: true,
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
		EVMSplit:         true,
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

// TestCompositeIterationRoutesToEVMStoreUnderSplit verifies iteration on EVM
// keys routes to evmStore under EVMSplit, matching Get/Has. Specifically
// covers the case where evmStore has the data and cosmosStore does not — which
// is always true under Split since writes go exclusively to one backend.
func TestCompositeIterationRoutesToEVMStoreUnderSplit(t *testing.T) {
	dir, err := os.MkdirTemp("", "composite_iter_readmode_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		EVMSplit:         true,
		EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
	}
	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	// Pointer-registry-style keys: legacy bucket prefix 0x15 with a versioned suffix.
	prefix := []byte{0x15, 0x01, 0xAA}
	v1Key := append(append([]byte{}, prefix...), 0x00, 0x01)
	v2Key := append(append([]byte{}, prefix...), 0x00, 0x02)

	// Write ONLY to evmStore to model Split-mode state (cosmos has no evm data).
	cs := []*proto.NamedChangeSet{{
		Name: evm.EVMStoreKey,
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{Key: v1Key, Value: []byte("addr_v1")},
				{Key: v2Key, Value: []byte("addr_v2")},
			},
		},
	}}
	require.NoError(t, store.evmStore.ApplyChangesetSync(1, cs))

	// Under EVMSplit, iteration must trust evmStore. The buggy WriteMode-based
	// guard would route to cosmosStore here and return empty.
	end := append(append([]byte{}, prefix...), 0xFF, 0xFF)
	iter, err := store.ReverseIterator(evm.EVMStoreKey, 1, prefix, end)
	require.NoError(t, err)
	defer iter.Close()

	require.True(t, iter.Valid(), "expected iteration to find data in evmStore under EVMSplit")
	require.Equal(t, v2Key, iter.Key())
	require.Equal(t, []byte("addr_v2"), iter.Value())

	iter.Next()
	require.True(t, iter.Valid())
	require.Equal(t, v1Key, iter.Key())
	require.Equal(t, []byte("addr_v1"), iter.Value())
}

// TestCompositeIteration_EVMSplit_Pointers covers the canonical
// Giga production config. Under EVMSplit, writes via the composite strip
// evm from cosmos — so iteration MUST route to evmStore to find the data.
func TestCompositeIteration_EVMSplit_Pointers(t *testing.T) {
	dir, err := os.MkdirTemp("", "composite_iter_split_split_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
		EVMSplit:         true,
		EVMDBDirectory:   filepath.Join(dir, "evm_ss"),
	}
	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	prefix := []byte{0x15, 0x01, 0xBB}
	v1Key := append(append([]byte{}, prefix...), 0x00, 0x01)
	v2Key := append(append([]byte{}, prefix...), 0x00, 0x02)

	cs := []*proto.NamedChangeSet{{
		Name: evm.EVMStoreKey,
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{Key: v1Key, Value: []byte("addr_v1")},
				{Key: v2Key, Value: []byte("addr_v2")},
			},
		},
	}}
	require.NoError(t, store.ApplyChangesetSync(1, cs))

	end := append(append([]byte{}, prefix...), 0xFF, 0xFF)
	iter, err := store.ReverseIterator(evm.EVMStoreKey, 1, prefix, end)
	require.NoError(t, err)
	defer iter.Close()

	require.True(t, iter.Valid(), "EVMSplit: iteration must find evm data")
	require.Equal(t, v2Key, iter.Key())
	require.Equal(t, []byte("addr_v2"), iter.Value())

	iter.Next()
	require.True(t, iter.Valid())
	require.Equal(t, v1Key, iter.Key())
}

// TestCompositeIteration_SeparateDBs_EVMSplit exercises the full
// routing stack: composite → evmStore (separateDBs=true) → Legacy sub-DB.
// Verifies pointer iteration works end-to-end with SeparateEVMSubDBs enabled.
func TestCompositeIteration_SeparateDBs_EVMSplit(t *testing.T) {
	dir, err := os.MkdirTemp("", "composite_iter_sepdb_test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	ssConfig := config.StateStoreConfig{
		Backend:           "pebbledb",
		AsyncWriteBuffer:  0,
		KeepRecent:        100000,
		EVMSplit:          true,
		EVMDBDirectory:    filepath.Join(dir, "evm_ss"),
		SeparateEVMSubDBs: true,
	}
	store, err := NewCompositeStateStore(ssConfig, dir)
	require.NoError(t, err)
	defer store.Close()

	prefix := []byte{0x15, 0x01, 0xCC}
	v1Key := append(append([]byte{}, prefix...), 0x00, 0x01)
	v2Key := append(append([]byte{}, prefix...), 0x00, 0x02)

	cs := []*proto.NamedChangeSet{{
		Name: evm.EVMStoreKey,
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{Key: v1Key, Value: []byte("addr_v1")},
				{Key: v2Key, Value: []byte("addr_v2")},
			},
		},
	}}
	require.NoError(t, store.ApplyChangesetSync(1, cs))

	end := append(append([]byte{}, prefix...), 0xFF, 0xFF)
	iter, err := store.ReverseIterator(evm.EVMStoreKey, 1, prefix, end)
	require.NoError(t, err, "separate-DB mode must support iteration within a bucket")
	defer iter.Close()

	require.True(t, iter.Valid())
	require.Equal(t, v2Key, iter.Key())
	require.Equal(t, []byte("addr_v2"), iter.Value())

	iter.Next()
	require.True(t, iter.Valid())
	require.Equal(t, v1Key, iter.Key())
}
