package mvcc

import (
	"testing"

	"github.com/stretchr/testify/require"
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
