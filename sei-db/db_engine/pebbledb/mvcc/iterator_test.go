package mvcc

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
)

func TestIteratorDescendingIncludesPrefixExtendedKeys(t *testing.T) {
	const store = "store1"
	db := newTestDB(t, true)
	require.True(t, db.descending)

	applyVersion(t, db, store, 1, []byte("a"), []byte("v-a"))
	applyVersion(t, db, store, 1, []byte("aa"), []byte("v-aa"))
	applyVersion(t, db, store, 1, []byte("b"), []byte("v-b"))

	itr, err := db.Iterator(store, 1, nil, nil)
	require.NoError(t, err)
	defer func() { _ = itr.Close() }()

	var keys []string
	for ; itr.Valid(); itr.Next() {
		keys = append(keys, string(itr.Key()))
	}
	require.NoError(t, itr.Error())
	require.Equal(t, []string{"a", "aa", "b"}, keys)
}

func TestIteratorDescendingDefaultComparerAdvancesForward(t *testing.T) {
	cfg := config.DefaultStateStoreConfig()
	cfg.UseDefaultComparer = true

	store, err := OpenDB(t.TempDir(), cfg)
	require.NoError(t, err)

	db := store.(*Database)
	t.Cleanup(func() { _ = db.Close() })
	require.True(t, db.descending)

	const storeKey = "evm"
	prefix := []byte{0x15, 0x01, 0xaa}
	key1 := append(append([]byte{}, prefix...), 0x00, 0x01)
	key2 := append(append([]byte{}, key1...), 0x03)
	key3 := append(append([]byte{}, prefix...), 0x00, 0x02)

	applyVersion(t, db, storeKey, 1, key1, []byte("v1"))
	applyVersion(t, db, storeKey, 1, key2, []byte("v2"))
	applyVersion(t, db, storeKey, 1, key3, []byte("v3"))

	end := append(append([]byte{}, prefix...), 0xff)
	itr, err := db.Iterator(storeKey, 1, prefix, end)
	require.NoError(t, err)
	defer func() { _ = itr.Close() }()

	var keys [][]byte
	for i := 0; itr.Valid() && i < 10; i++ {
		keys = append(keys, itr.Key())
		itr.Next()
	}
	require.NoError(t, itr.Error())
	require.Equal(t, [][]byte{key1, key2, key3}, keys)
}
