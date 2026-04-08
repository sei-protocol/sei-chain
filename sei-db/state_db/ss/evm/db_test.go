package evm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/backend"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

func testConfig() config.StateStoreConfig {
	return config.StateStoreConfig{
		Backend:          "pebbledb",
		AsyncWriteBuffer: 0,
		KeepRecent:       100000,
	}
}

func openTestStore(t *testing.T) types.StateStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewEVMStateStore(dir, testConfig())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestEVMStateStoreDefaultUsesUnifiedDB(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()

	store, err := NewEVMStateStore(dir, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.False(t, store.separateDBs)
	require.Len(t, store.managedDBs, 1)

	for _, storeType := range AllEVMStoreTypes() {
		require.Same(t, store.managedDBs[0], store.subDBs[storeType])
		_, err := os.Stat(filepath.Join(dir, StoreTypeName(storeType)))
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestEVMStateStoreSeparatedPreservesUnifiedKeyLayout(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()
	cfg.SeparateEVMSubDBs = true

	store, err := NewEVMStateStore(dir, cfg)
	require.NoError(t, err)

	addr := make([]byte, 20)
	addr[0] = 0x11
	slot := make([]byte, 32)
	slot[0] = 0x22
	nonceKey := append([]byte{0x0a}, addr...)
	storageKey := append([]byte{0x03}, append(addr, slot...)...)

	cs := []*proto.NamedChangeSet{
		{
			Name: EVMStoreKey,
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: nonceKey, Value: []byte{0x09}},
					{Key: storageKey, Value: []byte("slot_value")},
				},
			},
		},
	}
	require.NoError(t, store.ApplyChangesetSync(7, cs))
	require.NoError(t, store.SetLatestVersion(7))
	require.NoError(t, store.Close())

	opener := backend.ResolveBackend(cfg.Backend)

	nonceDir := filepath.Join(dir, StoreTypeName(StoreNonce))
	nonceDB, err := opener(nonceDir, subDBConfig(cfg, nonceDir))
	require.NoError(t, err)
	defer nonceDB.Close()

	nonceVal, err := nonceDB.Get(EVMStoreKey, 7, nonceKey)
	require.NoError(t, err)
	require.Equal(t, []byte{0x09}, nonceVal)
	require.Equal(t, int64(7), nonceDB.GetLatestVersion())

	_, strippedNonceKey := commonevm.ParseEVMKey(nonceKey)
	rewrittenNonceVal, err := nonceDB.Get(StoreTypeName(StoreNonce), 7, strippedNonceKey)
	require.NoError(t, err)
	require.Nil(t, rewrittenNonceVal, "separated DB should preserve evm store key and full key layout")

	storageDir := filepath.Join(dir, StoreTypeName(StoreStorage))
	storageDB, err := opener(storageDir, subDBConfig(cfg, storageDir))
	require.NoError(t, err)
	defer storageDB.Close()

	storageVal, err := storageDB.Get(EVMStoreKey, 7, storageKey)
	require.NoError(t, err)
	require.Equal(t, []byte("slot_value"), storageVal)
}

// verifyStateStoreInterface ensures EVMStateStore satisfies db_engine.StateStore.
func TestEVMStateStoreImplementsInterface(t *testing.T) {
	var _ types.StateStore = (*EVMStateStore)(nil)
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
