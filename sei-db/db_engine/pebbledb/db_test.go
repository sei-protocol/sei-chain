package pebbledb

import (
	"testing"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/dbcache"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// forEachCacheMode runs fn once with a warm cache and once with caching disabled,
// so cache-sensitive tests exercise both the cache and the raw storage layer.
func forEachCacheMode(t *testing.T, fn func(t *testing.T, cfg PebbleDBConfig, cacheCfg dbcache.CacheConfig)) {
	for _, mode := range []struct {
		name      string
		cacheSize uint64
	}{
		{"cached", 16 * unit.MB},
		{"uncached", 0},
	} {
		t.Run(mode.name, func(t *testing.T) {
			cfg := DefaultTestConfig(t)
			cacheCfg := DefaultTestCacheConfig()
			cacheCfg.MaxSize = mode.cacheSize
			fn(t, cfg, cacheCfg)
		})
	}
}

func openDB(t *testing.T, cfg *PebbleDBConfig, cacheCfg *dbcache.CacheConfig) types.KeyValueDB {
	t.Helper()
	db, err := OpenWithCache(t.Context(), cfg, cacheCfg,
		threading.NewAdHocPool(), threading.NewAdHocPool())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

// ---------------------------------------------------------------------------
// Cache-sensitive tests — run in both cached and uncached modes
// ---------------------------------------------------------------------------

func TestDBGetSetDelete(t *testing.T) {
	forEachCacheMode(t, func(t *testing.T, cfg PebbleDBConfig, cacheCfg dbcache.CacheConfig) {
		db := openDB(t, &cfg, &cacheCfg)

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
	})
}

func TestBatchAtomicWrite(t *testing.T) {
	forEachCacheMode(t, func(t *testing.T, cfg PebbleDBConfig, cacheCfg dbcache.CacheConfig) {
		db := openDB(t, &cfg, &cacheCfg)

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
	})
}

func TestErrNotFoundConsistency(t *testing.T) {
	forEachCacheMode(t, func(t *testing.T, cfg PebbleDBConfig, cacheCfg dbcache.CacheConfig) {
		db := openDB(t, &cfg, &cacheCfg)

		_, err := db.Get([]byte("missing-key"))
		require.Error(t, err)
		require.ErrorIs(t, err, errorutils.ErrNotFound)
		require.True(t, errorutils.IsNotFound(err))
	})
}

func TestGetReturnsCopy(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cacheCfg := DefaultTestCacheConfig()
	cacheCfg.MaxSize = 0
	db := openDB(t, &cfg, &cacheCfg)

	require.NoError(t, db.Set([]byte("k"), []byte("v"), types.WriteOptions{Sync: false}))

	got, err := db.Get([]byte("k"))
	require.NoError(t, err)
	got[0] = 'X'

	got2, err := db.Get([]byte("k"))
	require.NoError(t, err)
	require.Equal(t, "v", string(got2), "stored value should remain unchanged")
}

func TestBatchLenResetDelete(t *testing.T) {
	forEachCacheMode(t, func(t *testing.T, cfg PebbleDBConfig, cacheCfg dbcache.CacheConfig) {
		db := openDB(t, &cfg, &cacheCfg)

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
	})
}

func TestFlush(t *testing.T) {
	forEachCacheMode(t, func(t *testing.T, cfg PebbleDBConfig, cacheCfg dbcache.CacheConfig) {
		db := openDB(t, &cfg, &cacheCfg)

		require.NoError(t, db.Set([]byte("flush-test"), []byte("val"), types.WriteOptions{Sync: false}))
		require.NoError(t, db.Flush())

		got, err := db.Get([]byte("flush-test"))
		require.NoError(t, err)
		require.Equal(t, "val", string(got))
	})
}

// ---------------------------------------------------------------------------
// Cache-irrelevant tests — iterators and lifecycle, run once
// ---------------------------------------------------------------------------

func TestIteratorBounds(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cacheCfg := DefaultTestCacheConfig()
	db := openDB(t, &cfg, &cacheCfg)

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
	cfg := DefaultTestConfig(t)
	cacheCfg := DefaultTestCacheConfig()
	db := openDB(t, &cfg, &cacheCfg)

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

func TestIteratorSeekLTAndValue(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cacheCfg := DefaultTestCacheConfig()
	db := openDB(t, &cfg, &cacheCfg)

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

func TestCloseIsIdempotent(t *testing.T) {
	cfg := DefaultTestConfig(t)
	cacheCfg := DefaultTestCacheConfig()
	db, err := OpenWithCache(t.Context(), &cfg, &cacheCfg,
		threading.NewAdHocPool(), threading.NewAdHocPool())
	require.NoError(t, err)

	require.NoError(t, db.Close())
	require.NoError(t, db.Close(), "second Close should be idempotent")
}
