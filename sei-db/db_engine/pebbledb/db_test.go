package pebbledb

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cockroachdb/pebble/v2"
	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

func openDB(t *testing.T, cfg *PebbleDBConfig) types.KeyValueDB {
	t.Helper()
	db, err := Open(t.Context(), cfg, pebble.DefaultComparer)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

// ---------------------------------------------------------------------------
// Basic CRUD
// ---------------------------------------------------------------------------

func TestDBGetSetDelete(t *testing.T) {
	cfg := DefaultTestPebbleDBConfig(t)
	db := openDB(t, cfg)

	key := []byte("k1")
	val := []byte("v1")

	_, err := db.Get(key)
	require.ErrorIs(t, err, errorutils.ErrNotFound)

	require.NoError(t, db.Set(key, val, types.WriteOptions{Sync: false}))

	got, err := db.Get(key)
	require.NoError(t, err)
	require.Equal(t, val, got)

	require.NoError(t, db.Delete(key, types.WriteOptions{Sync: false}))

	_, err = db.Get(key)
	require.ErrorIs(t, err, errorutils.ErrNotFound)
}

func TestBatchAtomicWrite(t *testing.T) {
	cfg := DefaultTestPebbleDBConfig(t)
	db := openDB(t, cfg)

	b := db.NewBatch()
	t.Cleanup(func() { require.NoError(t, b.Close()) })

	require.NoError(t, b.Set([]byte("a"), []byte("1")))
	require.NoError(t, b.Set([]byte("b"), []byte("2")))
	require.NoError(t, b.Commit(types.WriteOptions{Sync: false}))

	for _, tc := range []struct{ k, v string }{{"a", "1"}, {"b", "2"}} {
		got, err := db.Get([]byte(tc.k))
		require.NoError(t, err, "key=%q", tc.k)
		require.Equal(t, tc.v, string(got), "key=%q", tc.k)
	}
}

func TestErrNotFoundConsistency(t *testing.T) {
	cfg := DefaultTestPebbleDBConfig(t)
	db := openDB(t, cfg)

	_, err := db.Get([]byte("missing-key"))
	require.Error(t, err)
	require.ErrorIs(t, err, errorutils.ErrNotFound)
	require.True(t, errorutils.IsNotFound(err))
}

func TestGetReturnsCopy(t *testing.T) {
	cfg := DefaultTestPebbleDBConfig(t)
	db := openDB(t, cfg)

	require.NoError(t, db.Set([]byte("k"), []byte("v"), types.WriteOptions{Sync: false}))

	got, err := db.Get([]byte("k"))
	require.NoError(t, err)
	got[0] = 'X'

	got2, err := db.Get([]byte("k"))
	require.NoError(t, err)
	require.Equal(t, "v", string(got2), "stored value should remain unchanged")
}

func TestBatchLenResetDelete(t *testing.T) {
	cfg := DefaultTestPebbleDBConfig(t)
	db := openDB(t, cfg)

	require.NoError(t, db.Set([]byte("to-delete"), []byte("val"), types.WriteOptions{Sync: false}))

	b := db.NewBatch()
	t.Cleanup(func() { require.NoError(t, b.Close()) })

	initialLen := b.Len()

	require.NoError(t, b.Set([]byte("a"), []byte("1")))
	require.NoError(t, b.Delete([]byte("to-delete")))
	require.Greater(t, b.Len(), initialLen)

	b.Reset()
	require.Equal(t, initialLen, b.Len())

	require.NoError(t, b.Set([]byte("b"), []byte("2")))
	require.NoError(t, b.Commit(types.WriteOptions{Sync: false}))

	got, err := db.Get([]byte("b"))
	require.NoError(t, err)
	require.Equal(t, "2", string(got))
}

func TestFlush(t *testing.T) {
	cfg := DefaultTestPebbleDBConfig(t)
	db := openDB(t, cfg)

	require.NoError(t, db.Set([]byte("flush-test"), []byte("val"), types.WriteOptions{Sync: false}))
	require.NoError(t, db.Flush())

	got, err := db.Get([]byte("flush-test"))
	require.NoError(t, err)
	require.Equal(t, "val", string(got))
}

// ---------------------------------------------------------------------------
// Iterators
// ---------------------------------------------------------------------------

func TestIteratorBounds(t *testing.T) {
	cfg := DefaultTestPebbleDBConfig(t)
	db := openDB(t, cfg)

	for _, k := range []string{"a", "b", "c"} {
		require.NoError(t, db.Set([]byte(k), []byte("x"), types.WriteOptions{Sync: false}))
	}

	itr, err := db.NewIter(&types.IterOptions{LowerBound: []byte("b"), UpperBound: []byte("d")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, itr.Close()) })

	var keys []string
	for ok := itr.First(); ok && itr.Valid(); ok = itr.Next() {
		keys = append(keys, string(itr.Key()))
	}
	require.NoError(t, itr.Error())
	require.Equal(t, []string{"b", "c"}, keys)
}

func TestIteratorPrev(t *testing.T) {
	cfg := DefaultTestPebbleDBConfig(t)
	db := openDB(t, cfg)

	for _, k := range []string{"a", "b", "c"} {
		require.NoError(t, db.Set([]byte(k), []byte("x"), types.WriteOptions{Sync: false}))
	}

	itr, err := db.NewIter(nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, itr.Close()) })

	require.True(t, itr.Last())
	require.True(t, itr.Valid())
	require.Equal(t, "c", string(itr.Key()))

	require.True(t, itr.Prev())
	require.True(t, itr.Valid())
	require.Equal(t, "b", string(itr.Key()))
}

func TestIteratorNextPrefixWithComparerSplit(t *testing.T) {
	cmp := *pebble.DefaultComparer
	cmp.Name = "sei-db/test-split-on-slash"
	cmp.Split = func(k []byte) int {
		for i, b := range k {
			if b == '/' {
				return i + 1
			}
		}
		return len(k)
	}
	cmp.ImmediateSuccessor = func(dst, a []byte) []byte {
		for i := len(a) - 1; i >= 0; i-- {
			if a[i] != 0xff {
				dst = append(dst, a[:i+1]...)
				dst[len(dst)-1]++
				return dst
			}
		}
		return append(dst, a...)
	}

	cfg := DefaultTestPebbleDBConfig(t)
	db, err := Open(t.Context(), cfg, &cmp)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	for _, k := range []string{"a/1", "a/2", "a/3", "b/1"} {
		require.NoError(t, db.Set([]byte(k), []byte("x"), types.WriteOptions{Sync: false}))
	}

	itr, err := db.NewIter(nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, itr.Close()) })

	require.True(t, itr.SeekGE([]byte("a/")))
	require.True(t, itr.Valid())
	require.True(t, bytes.HasPrefix(itr.Key(), []byte("a/")))

	require.True(t, itr.NextPrefix())
	require.True(t, itr.Valid())
	require.Equal(t, "b/1", string(itr.Key()))
}

func TestIteratorSeekLTAndValue(t *testing.T) {
	cfg := DefaultTestPebbleDBConfig(t)
	db := openDB(t, cfg)

	for _, kv := range []struct{ k, v string }{
		{"a", "val-a"},
		{"b", "val-b"},
		{"c", "val-c"},
	} {
		require.NoError(t, db.Set([]byte(kv.k), []byte(kv.v), types.WriteOptions{Sync: false}))
	}

	itr, err := db.NewIter(nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, itr.Close()) })

	require.True(t, itr.SeekLT([]byte("c")))
	require.True(t, itr.Valid())
	require.Equal(t, "b", string(itr.Key()))
	require.Equal(t, "val-b", string(itr.Value()))
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

func TestCloseIsIdempotent(t *testing.T) {
	cfg := DefaultTestPebbleDBConfig(t)
	db, err := Open(t.Context(), cfg, pebble.DefaultComparer)
	require.NoError(t, err)

	require.NoError(t, db.Close())
	require.NoError(t, db.Close(), "second Close should be idempotent")
}
