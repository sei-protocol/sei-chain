package evm

import (
	"testing"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/stretchr/testify/require"
)

func testConfig() config.StateStoreConfig {
	return config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
	}
}

func openTestStore(t *testing.T) db_engine.StateStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewEVMStateStore(dir, testConfig(), logger.NewNopLogger())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

// verifyStateStoreInterface ensures EVMStateStore satisfies db_engine.StateStore.
func TestEVMStateStoreImplementsInterface(t *testing.T) {
	var _ db_engine.StateStore = (*EVMStateStore)(nil)
}

func TestEVMStateStoreGetHas(t *testing.T) {
	store := openTestStore(t)

	addr := make([]byte, 20)
	addr[0] = 0x01
	slot := make([]byte, 32)
	slot[0] = 0xAA
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	changesets := []*proto.NamedChangeSet{
		{
			Name: EVMStoreKey,
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: storageKey, Value: []byte("storage_value")},
				},
			},
		},
	}
	err := store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	val, err := store.Get(EVMStoreKey, 1, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("storage_value"), val)

	has, err := store.Has(EVMStoreKey, 1, storageKey)
	require.NoError(t, err)
	require.True(t, has)

	val, err = store.Get(EVMStoreKey, 1, []byte("nonexistent"))
	require.NoError(t, err)
	require.Nil(t, val)
}

func TestEVMStateStoreVersionHandling(t *testing.T) {
	store := openTestStore(t)

	addr := make([]byte, 20)
	addr[0] = 0x02
	nonceKey := append([]byte{0x0a}, addr...)

	for v := int64(1); v <= 5; v++ {
		cs := []*proto.NamedChangeSet{
			{
				Name: EVMStoreKey,
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: nonceKey, Value: []byte{byte(v)}},
					},
				},
			},
		}
		require.NoError(t, store.ApplyChangesetSync(v, cs))
	}

	for v := int64(1); v <= 5; v++ {
		val, err := store.Get(EVMStoreKey, v, nonceKey)
		require.NoError(t, err)
		require.Equal(t, []byte{byte(v)}, val, "version %d", v)
	}
}

func TestEVMStateStoreDeleteTombstone(t *testing.T) {
	store := openTestStore(t)

	addr := make([]byte, 20)
	addr[0] = 0x03
	codeKey := append([]byte{0x07}, addr...)

	cs := []*proto.NamedChangeSet{
		{
			Name: EVMStoreKey,
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: codeKey, Value: []byte{0x60, 0x80}},
				},
			},
		},
	}
	require.NoError(t, store.ApplyChangesetSync(1, cs))

	cs = []*proto.NamedChangeSet{
		{
			Name: EVMStoreKey,
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: codeKey, Delete: true},
				},
			},
		},
	}
	require.NoError(t, store.ApplyChangesetSync(2, cs))

	val, err := store.Get(EVMStoreKey, 1, codeKey)
	require.NoError(t, err)
	require.Equal(t, []byte{0x60, 0x80}, val)

	val, err = store.Get(EVMStoreKey, 2, codeKey)
	require.NoError(t, err)
	require.Nil(t, val)
}

func TestEVMStateStoreMultipleSubDBs(t *testing.T) {
	store := openTestStore(t)

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
	storageKey := append([]byte{0x03}, append(addr, slot...)...)
	legacyKey := append([]byte{0x01}, addr...)

	cs := []*proto.NamedChangeSet{
		{
			Name: EVMStoreKey,
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: nonceKey, Value: []byte{0x05}},
					{Key: codeHashKey, Value: []byte("hash_abc")},
					{Key: codeKey, Value: []byte{0x60, 0x80}},
					{Key: storageKey, Value: []byte("slot_val")},
					{Key: legacyKey, Value: []byte("sei1abc")},
				},
			},
		},
	}
	require.NoError(t, store.ApplyChangesetSync(1, cs))

	tests := []struct {
		name    string
		fullKey []byte
		value   []byte
	}{
		{"Nonce", nonceKey, []byte{0x05}},
		{"CodeHash", codeHashKey, []byte("hash_abc")},
		{"Code", codeKey, []byte{0x60, 0x80}},
		{"Storage", storageKey, []byte("slot_val")},
		{"Legacy", legacyKey, []byte("sei1abc")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val, err := store.Get(EVMStoreKey, 1, tc.fullKey)
			require.NoError(t, err)
			require.Equal(t, tc.value, val)
		})
	}
}

func TestEVMStateStoreVersionTracking(t *testing.T) {
	store := openTestStore(t)

	require.Equal(t, int64(0), store.GetLatestVersion())
	require.Equal(t, int64(0), store.GetEarliestVersion())

	addr := make([]byte, 20)
	nonceKey := append([]byte{0x0a}, addr...)

	for v := int64(1); v <= 3; v++ {
		cs := []*proto.NamedChangeSet{
			{
				Name: EVMStoreKey,
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: nonceKey, Value: []byte{byte(v)}},
					},
				},
			},
		}
		require.NoError(t, store.ApplyChangesetSync(v, cs))
		require.NoError(t, store.SetLatestVersion(v))
	}

	require.Equal(t, int64(3), store.GetLatestVersion())

	require.NoError(t, store.SetEarliestVersion(2, false))
	require.Equal(t, int64(2), store.GetEarliestVersion())
}

func TestEVMStateStorePrune(t *testing.T) {
	store := openTestStore(t)

	addr := make([]byte, 20)
	addr[0] = 0x01
	slot := make([]byte, 32)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	for v := int64(1); v <= 10; v++ {
		cs := []*proto.NamedChangeSet{
			{
				Name: EVMStoreKey,
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: storageKey, Value: []byte{byte(v)}},
					},
				},
			},
		}
		require.NoError(t, store.ApplyChangesetSync(v, cs))
		require.NoError(t, store.SetLatestVersion(v))
	}

	require.NoError(t, store.Prune(5))

	val, err := store.Get(EVMStoreKey, 6, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte{6}, val, "version 6 should survive prune")

	val, err = store.Get(EVMStoreKey, 10, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte{10}, val, "version 10 should survive prune")
}

func TestEVMStateStoreNonEVMChangesetsIgnored(t *testing.T) {
	store := openTestStore(t)

	cs := []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("balance"), Value: []byte("100")},
				},
			},
		},
	}
	require.NoError(t, store.ApplyChangesetSync(1, cs))
}

func TestParseKey(t *testing.T) {
	t.Run("Parse storage key", func(t *testing.T) {
		addr := make([]byte, 20)
		slot := make([]byte, 32)
		key := append([]byte{0x03}, append(addr, slot...)...)

		storeType, stripped := commonevm.ParseEVMKey(key)
		require.Equal(t, StoreStorage, storeType)
		require.Equal(t, append(addr, slot...), stripped)
	})

	t.Run("Parse nonce key", func(t *testing.T) {
		addr := make([]byte, 20)
		key := append([]byte{0x0a}, addr...)

		storeType, stripped := commonevm.ParseEVMKey(key)
		require.Equal(t, StoreNonce, storeType)
		require.Equal(t, addr, stripped)
	})

	t.Run("Parse code key", func(t *testing.T) {
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
		require.Equal(t, key, keyBytes)
	})

	t.Run("Malformed key goes to legacy", func(t *testing.T) {
		key := []byte{0x03, 0x01, 0x02}

		storeType, keyBytes := commonevm.ParseEVMKey(key)
		require.Equal(t, StoreLegacy, storeType)
		require.Equal(t, key, keyBytes)
	})

	t.Run("Parse codesize key goes to legacy", func(t *testing.T) {
		addr := make([]byte, 20)
		addr[0] = 0x42
		key := append([]byte{0x09}, addr...)

		storeType, keyBytes := commonevm.ParseEVMKey(key)
		require.Equal(t, StoreLegacy, storeType)
		require.Equal(t, key, keyBytes)
	})
}

func TestCodeSizeGoesToLegacyDB(t *testing.T) {
	store := openTestStore(t)

	addr := make([]byte, 20)
	addr[0] = 0x42
	codeSizeKey := append([]byte{0x09}, addr...)

	cs := []*proto.NamedChangeSet{
		{
			Name: EVMStoreKey,
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: codeSizeKey, Value: []byte{0x00, 0x10}},
				},
			},
		},
	}
	require.NoError(t, store.ApplyChangesetSync(1, cs))

	val, err := store.Get(EVMStoreKey, 1, codeSizeKey)
	require.NoError(t, err)
	require.Equal(t, []byte{0x00, 0x10}, val)
}
