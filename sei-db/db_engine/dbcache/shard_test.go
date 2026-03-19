package dbcache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newTestShard(t *testing.T, maxSize uint64, store map[string][]byte) *shard {
	t.Helper()
	read := Reader(func(key []byte) ([]byte, bool, error) {
		v, ok := store[string(key)]
		if !ok {
			return nil, false, nil
		}
		return v, true, nil
	})
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = 0
	s, err := NewShard(context.Background(), config, read, threading.NewAdHocPool(), maxSize)
	require.NoError(t, err)
	return s
}

// ---------------------------------------------------------------------------
// NewShard
// ---------------------------------------------------------------------------

func TestNewShardValid(t *testing.T) {
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = 0
	read := Reader(func(key []byte) ([]byte, bool, error) { return nil, false, nil })
	s, err := NewShard(context.Background(), config, read, threading.NewAdHocPool(), 1024)
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestNewShardZeroMaxSize(t *testing.T) {
	config := DefaultTestCacheConfig()
	read := Reader(func(key []byte) ([]byte, bool, error) { return nil, false, nil })
	_, err := NewShard(context.Background(), config, read, threading.NewAdHocPool(), 0)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Get — cache miss flows
// ---------------------------------------------------------------------------

func TestGetCacheMissFoundInDB(t *testing.T) {
	store := map[string][]byte{"hello": []byte("world")}
	s := newTestShard(t, 4096, store)

	val, found, err := s.Get([]byte("hello"), s.currentVersion, true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "world", string(val))
}

func TestGetCacheMissNotFoundInDB(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	val, found, err := s.Get([]byte("missing"), s.currentVersion, true)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

func TestGetCacheMissDBError(t *testing.T) {
	dbErr := errors.New("disk on fire")
	readFunc := Reader(func(key []byte) ([]byte, bool, error) { return nil, false, dbErr })
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = 0
	s, _ := NewShard(context.Background(), config, readFunc, threading.NewAdHocPool(), 4096)

	_, _, err := s.Get([]byte("boom"), s.currentVersion, true)
	require.Error(t, err)
	require.ErrorIs(t, err, dbErr)
}

func TestGetDBErrorDoesNotCacheResult(t *testing.T) {
	var calls atomic.Int64
	readFunc := Reader(func(key []byte) ([]byte, bool, error) {
		n := calls.Add(1)
		if n == 1 {
			return nil, false, errors.New("transient")
		}
		return []byte("recovered"), true, nil
	})
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = 0
	s, _ := NewShard(context.Background(), config, readFunc, threading.NewAdHocPool(), 4096)

	_, _, err := s.Get([]byte("key"), s.currentVersion, true)
	require.Error(t, err, "first call should fail")

	val, found, err := s.Get([]byte("key"), s.currentVersion, true)
	require.NoError(t, err, "second call should succeed")
	require.True(t, found)
	require.Equal(t, "recovered", string(val))
	require.Equal(t, int64(2), calls.Load(), "error should not be cached")
}

// ---------------------------------------------------------------------------
// Get — cache hit flows
// ---------------------------------------------------------------------------

func TestGetCacheHitAvailable(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{"k": []byte("v")})

	s.Get([]byte("k"), s.currentVersion, true)

	val, found, err := s.Get([]byte("k"), s.currentVersion, true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v", string(val))
}

func TestGetCacheHitDeleted(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.Get([]byte("gone"), s.currentVersion, true)

	val, found, err := s.Get([]byte("gone"), s.currentVersion, true)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

func TestGetAfterSet(t *testing.T) {
	var readCalls atomic.Int64
	readFunc := Reader(func(key []byte) ([]byte, bool, error) {
		readCalls.Add(1)
		return nil, false, nil
	})
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = 0
	s, _ := NewShard(context.Background(), config, readFunc, threading.NewAdHocPool(), 4096)

	s.Set([]byte("k"), []byte("from-set"))

	val, found, err := s.Get([]byte("k"), s.currentVersion, true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "from-set", string(val))
	require.Equal(t, int64(0), readCalls.Load(), "readFunc should not be called for Set-populated entry")
}

func TestGetAfterDelete(t *testing.T) {
	store := map[string][]byte{"k": []byte("v")}
	s := newTestShard(t, 4096, store)

	// Warm the cache so the key is present before deleting.
	_, _, err := s.Get([]byte("k"), s.currentVersion, true)
	require.NoError(t, err)

	s.Delete([]byte("k"))

	val, found, err := s.Get([]byte("k"), s.currentVersion, true)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

// ---------------------------------------------------------------------------
// Get — concurrent reads on the same key
// ---------------------------------------------------------------------------

func TestGetConcurrentSameKey(t *testing.T) {
	var readCalls atomic.Int64
	gate := make(chan struct{})

	readFunc := Reader(func(key []byte) ([]byte, bool, error) {
		readCalls.Add(1)
		<-gate
		return []byte("value"), true, nil
	})
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = 0
	s, _ := NewShard(context.Background(), config, readFunc, threading.NewAdHocPool(), 4096)

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	vals := make([]string, n)
	founds := make([]bool, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			v, f, e := s.Get([]byte("shared"), s.currentVersion, true)
			vals[idx] = string(v)
			founds[idx] = f
			errs[idx] = e
		}(i)
	}

	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i], "goroutine %d", i)
		require.True(t, founds[i], "goroutine %d", i)
		require.Equal(t, "value", vals[i], "goroutine %d", i)
	}

	require.Equal(t, int64(1), readCalls.Load(), "readFunc should be called exactly once")
}

// ---------------------------------------------------------------------------
// Get — context cancellation
// ---------------------------------------------------------------------------

func TestGetContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	readFunc := Reader(func(key []byte) ([]byte, bool, error) {
		time.Sleep(time.Second)
		return []byte("late"), true, nil
	})
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = 0
	s, _ := NewShard(ctx, config, readFunc, threading.NewAdHocPool(), 4096)

	cancel()

	_, _, err := s.Get([]byte("k"), s.currentVersion, true)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Get — updateLru flag
// ---------------------------------------------------------------------------

func TestGetUpdateLruTrue(t *testing.T) {
	store := map[string][]byte{
		"a": []byte("1"),
		"b": []byte("2"),
	}
	s := newTestShard(t, 4096, store)

	s.Get([]byte("a"), s.currentVersion, true)
	s.Get([]byte("b"), s.currentVersion, true)

	s.Get([]byte("a"), s.currentVersion, true)

	s.lock.Lock()
	lru := s.dbCacheGCQueue.PopLeastRecentlyUsed()
	s.lock.Unlock()

	require.Equal(t, "b", lru)
}

func TestGetUpdateLruFalse(t *testing.T) {
	store := map[string][]byte{
		"a": []byte("1"),
		"b": []byte("2"),
	}
	s := newTestShard(t, 4096, store)

	s.Get([]byte("a"), s.currentVersion, true)
	s.Get([]byte("b"), s.currentVersion, true)

	s.Get([]byte("a"), s.currentVersion, false)

	s.lock.Lock()
	lru := s.dbCacheGCQueue.PopLeastRecentlyUsed()
	s.lock.Unlock()

	require.Equal(t, "a", lru, "updateLru=false should not move entry")
}

// ---------------------------------------------------------------------------
// Set
// ---------------------------------------------------------------------------

func TestSetNewKey(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.Set([]byte("k"), []byte("v"))

	val, found, err := s.Get([]byte("k"), s.currentVersion, false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v", string(val))
}

func TestSetOverwritesExistingKey(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.Set([]byte("k"), []byte("old"))
	s.Set([]byte("k"), []byte("new"))

	val, found, err := s.Get([]byte("k"), s.currentVersion, false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "new", string(val))
}

func TestSetOverwritesDeletedKey(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.Delete([]byte("k"))
	s.Set([]byte("k"), []byte("revived"))

	val, found, err := s.Get([]byte("k"), s.currentVersion, false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "revived", string(val))
}

func TestSetNilValue(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.Set([]byte("k"), nil)

	val, found, err := s.Get([]byte("k"), s.currentVersion, false)
	require.NoError(t, err)
	require.False(t, found, "Set(key, nil) is equivalent to Delete")
	require.Nil(t, val)
}

func TestSetEmptyKey(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.Set([]byte(""), []byte("empty-key-val"))

	val, found, err := s.Get([]byte(""), s.currentVersion, false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "empty-key-val", string(val))
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestDeleteExistingKey(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.Set([]byte("k"), []byte("v"))
	s.Delete([]byte("k"))

	val, found, err := s.Get([]byte("k"), s.currentVersion, false)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

func TestDeleteNonexistentKey(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.Delete([]byte("ghost"))

	val, found, err := s.Get([]byte("ghost"), s.currentVersion, false)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

func TestDeleteThenSetThenGet(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.Set([]byte("k"), []byte("v1"))
	s.Delete([]byte("k"))
	s.Set([]byte("k"), []byte("v2"))

	val, found, err := s.Get([]byte("k"), s.currentVersion, false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v2", string(val))
}

// ---------------------------------------------------------------------------
// BatchSet
// ---------------------------------------------------------------------------

func TestBatchSetSetsMultiple(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.BatchSet([]CacheUpdate{
		{Key: []byte("a"), Value: []byte("1")},
		{Key: []byte("b"), Value: []byte("2")},
		{Key: []byte("c"), Value: []byte("3")},
	})

	for _, tc := range []struct {
		key, want string
	}{{"a", "1"}, {"b", "2"}, {"c", "3"}} {
		val, found, err := s.Get([]byte(tc.key), s.currentVersion, false)
		require.NoError(t, err, "Get(%q)", tc.key)
		require.True(t, found, "Get(%q)", tc.key)
		require.Equal(t, tc.want, string(val), "Get(%q)", tc.key)
	}
}

func TestBatchSetMixedSetAndDelete(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.Set([]byte("keep"), []byte("v"))
	s.Set([]byte("remove"), []byte("v"))

	s.BatchSet([]CacheUpdate{
		{Key: []byte("keep"), Value: []byte("updated")},
		{Key: []byte("remove"), Value: nil},
		{Key: []byte("new"), Value: []byte("fresh")},
	})

	val, found, _ := s.Get([]byte("keep"), s.currentVersion, false)
	require.True(t, found)
	require.Equal(t, "updated", string(val))

	_, found, _ = s.Get([]byte("remove"), s.currentVersion, false)
	require.False(t, found, "expected remove to be deleted")

	val, found, _ = s.Get([]byte("new"), s.currentVersion, false)
	require.True(t, found)
	require.Equal(t, "fresh", string(val))
}

func TestBatchSetEmpty(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})
	s.BatchSet(nil)
	s.BatchSet([]CacheUpdate{})

	bytes, entries := s.getSizeInfo()
	require.Equal(t, uint64(0), bytes)
	require.Equal(t, uint64(0), entries)
}

// ---------------------------------------------------------------------------
// BatchGet
// ---------------------------------------------------------------------------

func TestBatchGetAllCached(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.Set([]byte("a"), []byte("1"))
	s.Set([]byte("b"), []byte("2"))

	keys := map[string]types.BatchGetResult{
		"a": {},
		"b": {},
	}
	require.NoError(t, s.BatchGet(keys, s.currentVersion))

	for k, want := range map[string]string{"a": "1", "b": "2"} {
		r := keys[k]
		require.True(t, r.IsFound(), "key=%q", k)
		require.Equal(t, want, string(r.Value), "key=%q", k)
	}
}

func TestBatchGetAllFromDB(t *testing.T) {
	store := map[string][]byte{"x": []byte("10"), "y": []byte("20")}
	s := newTestShard(t, 4096, store)

	keys := map[string]types.BatchGetResult{
		"x": {},
		"y": {},
	}
	require.NoError(t, s.BatchGet(keys, s.currentVersion))

	for k, want := range map[string]string{"x": "10", "y": "20"} {
		r := keys[k]
		require.True(t, r.IsFound(), "key=%q", k)
		require.Equal(t, want, string(r.Value), "key=%q", k)
	}
}

func TestBatchGetMixedCachedAndDB(t *testing.T) {
	store := map[string][]byte{"db-key": []byte("from-db")}
	s := newTestShard(t, 4096, store)

	s.Set([]byte("cached"), []byte("from-cache"))

	keys := map[string]types.BatchGetResult{
		"cached": {},
		"db-key": {},
	}
	require.NoError(t, s.BatchGet(keys, s.currentVersion))

	require.True(t, keys["cached"].IsFound())
	require.Equal(t, "from-cache", string(keys["cached"].Value))
	require.True(t, keys["db-key"].IsFound())
	require.Equal(t, "from-db", string(keys["db-key"].Value))
}

func TestBatchGetNotFoundKeys(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	keys := map[string]types.BatchGetResult{
		"nope": {},
	}
	require.NoError(t, s.BatchGet(keys, s.currentVersion))
	require.False(t, keys["nope"].IsFound())
}

func TestBatchGetDeletedKeys(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.Set([]byte("del"), []byte("v"))
	s.Delete([]byte("del"))

	keys := map[string]types.BatchGetResult{
		"del": {},
	}
	require.NoError(t, s.BatchGet(keys, s.currentVersion))
	require.False(t, keys["del"].IsFound())
}

func TestBatchGetDBError(t *testing.T) {
	dbErr := errors.New("broken")
	readFunc := Reader(func(key []byte) ([]byte, bool, error) { return nil, false, dbErr })
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = 0
	s, _ := NewShard(context.Background(), config, readFunc, threading.NewAdHocPool(), 4096)

	keys := map[string]types.BatchGetResult{
		"fail": {},
	}
	require.NoError(t, s.BatchGet(keys, s.currentVersion), "BatchGet itself should not fail")
	require.Error(t, keys["fail"].Error, "expected per-key error")
}

func TestBatchGetEmpty(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	keys := map[string]types.BatchGetResult{}
	require.NoError(t, s.BatchGet(keys, s.currentVersion))
}

func TestBatchGetCachesResults(t *testing.T) {
	var readCalls atomic.Int64
	store := map[string][]byte{"k": []byte("v")}
	readFunc := Reader(func(key []byte) ([]byte, bool, error) {
		readCalls.Add(1)
		v, ok := store[string(key)]
		return v, ok, nil
	})
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = 0
	s, _ := NewShard(context.Background(), config, readFunc, threading.NewAdHocPool(), 4096)

	keys := map[string]types.BatchGetResult{"k": {}}
	s.BatchGet(keys, s.currentVersion)

	time.Sleep(50 * time.Millisecond)

	val, found, err := s.Get([]byte("k"), s.currentVersion, false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v", string(val))
	require.Equal(t, int64(1), readCalls.Load(), "result should be cached")
}

// ---------------------------------------------------------------------------
// Eviction
// ---------------------------------------------------------------------------

func TestEvictionRespectMaxSize(t *testing.T) {
	store := map[string][]byte{
		"a": []byte("aaaaaaaaaa"),
		"b": []byte("bbbbbbbbbb"),
		"c": []byte("cccccccccc"),
	}
	s := newTestShard(t, 30, store)

	s.Get([]byte("a"), s.currentVersion, true)
	s.Get([]byte("b"), s.currentVersion, true)

	_, entries := s.getSizeInfo()
	require.Equal(t, uint64(2), entries)

	s.Get([]byte("c"), s.currentVersion, true)

	bytes, entries := s.getSizeInfo()
	require.LessOrEqual(t, bytes, uint64(30), "shard size should not exceed maxSize")
	require.Equal(t, uint64(2), entries)
}

func TestEvictionOrderIsLRU(t *testing.T) {
	store := map[string][]byte{
		"a": []byte("1111"),
		"b": []byte("2222"),
		"c": []byte("3333"),
		"d": []byte("4444"),
	}
	s := newTestShard(t, 15, store)

	s.Get([]byte("a"), s.currentVersion, true)
	s.Get([]byte("b"), s.currentVersion, true)
	s.Get([]byte("c"), s.currentVersion, true)

	s.Get([]byte("a"), s.currentVersion, true)

	s.Get([]byte("d"), s.currentVersion, true)

	s.lock.Lock()
	_, bExists := s.dbCache["b"]
	_, aExists := s.dbCache["a"]
	s.lock.Unlock()

	require.False(t, bExists, "expected 'b' to be evicted (it was LRU)")
	require.True(t, aExists, "expected 'a' to survive (it was recently touched)")
}

func TestEvictionOnDelete(t *testing.T) {
	store := map[string][]byte{"a": []byte("val")}
	s := newTestShard(t, 10, store)

	s.Get([]byte("a"), s.currentVersion, true)
	s.Get([]byte("longkey1"), s.currentVersion, true)

	bytes, _ := s.getSizeInfo()
	require.LessOrEqual(t, bytes, uint64(10), "size should not exceed maxSize")
}

func TestEvictionOnGetFromDB(t *testing.T) {
	store := map[string][]byte{
		"a": []byte("small"),
		"x": []byte("12345678901234567890"),
	}
	s := newTestShard(t, 25, store)

	s.Get([]byte("a"), s.currentVersion, true)

	s.Get([]byte("x"), s.currentVersion, true)

	time.Sleep(50 * time.Millisecond)

	bytes, _ := s.getSizeInfo()
	require.LessOrEqual(t, bytes, uint64(25), "size should not exceed maxSize after DB read")
}

// ---------------------------------------------------------------------------
// getSizeInfo
// ---------------------------------------------------------------------------

func TestGetSizeInfoEmpty(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})
	bytes, entries := s.getSizeInfo()
	require.Equal(t, uint64(0), bytes)
	require.Equal(t, uint64(0), entries)
}

func TestGetSizeInfoAfterSets(t *testing.T) {
	store := map[string][]byte{
		"ab":  []byte("cd"),
		"efg": []byte("hi"),
	}
	s := newTestShard(t, 4096, store)

	s.Get([]byte("ab"), s.currentVersion, true)
	s.Get([]byte("efg"), s.currentVersion, true)

	bytes, entries := s.getSizeInfo()
	require.Equal(t, uint64(2), entries)
	require.Equal(t, uint64(9), bytes)
}

// ---------------------------------------------------------------------------
// estimatedOverheadPerEntry
// ---------------------------------------------------------------------------

func TestOverheadIncludedInSizeAfterSet(t *testing.T) {
	const overhead = 100
	store := map[string][]byte{
		"ab":  []byte("cd"),
		"efg": []byte("hi"),
	}
	read := Reader(func(key []byte) ([]byte, bool, error) {
		v, ok := store[string(key)]
		return v, ok, nil
	})
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = overhead
	s, _ := NewShard(context.Background(), config, read, threading.NewAdHocPool(), 100_000)

	s.Get([]byte("ab"), s.currentVersion, true)
	s.Get([]byte("efg"), s.currentVersion, true)

	bytes, entries := s.getSizeInfo()
	require.Equal(t, uint64(2), entries)
	// (2+2+100) + (3+2+100) = 209
	require.Equal(t, uint64(209), bytes)
}

func TestOverheadIncludedInSizeAfterDelete(t *testing.T) {
	const overhead = 100
	store := map[string][]byte{"abc": []byte("val")}
	read := Reader(func(key []byte) ([]byte, bool, error) {
		v, ok := store[string(key)]
		return v, ok, nil
	})
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = overhead
	s, _ := NewShard(context.Background(), config, read, threading.NewAdHocPool(), 100_000)

	// Warm the cache so the key is present before deleting.
	_, _, err := s.Get([]byte("abc"), s.currentVersion, true)
	require.NoError(t, err)

	s.lock.Lock()
	s.deleteInDBCacheUnlocked([]byte("abc"))
	s.lock.Unlock()

	bytes, entries := s.getSizeInfo()
	require.Equal(t, uint64(1), entries)
	// 3 + 100 = 103
	require.Equal(t, uint64(103), bytes)
}

func TestOverheadIncludedInSizeAfterDBRead(t *testing.T) {
	const overhead = 100
	store := map[string][]byte{"key": []byte("value")}
	read := Reader(func(key []byte) ([]byte, bool, error) {
		v, ok := store[string(key)]
		return v, ok, nil
	})
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = overhead
	s, _ := NewShard(context.Background(), config, read, threading.NewAdHocPool(), 100_000)

	val, found, err := s.Get([]byte("key"), s.currentVersion, true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "value", string(val))

	bytes, entries := s.getSizeInfo()
	require.Equal(t, uint64(1), entries)
	// 3 + 5 + 100 = 108
	require.Equal(t, uint64(108), bytes)
}

func TestOverheadIncludedInSizeAfterDBReadNotFound(t *testing.T) {
	const overhead = 100
	read := Reader(func(key []byte) ([]byte, bool, error) { return nil, false, nil })
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = overhead
	s, _ := NewShard(context.Background(), config, read, threading.NewAdHocPool(), 100_000)

	_, found, err := s.Get([]byte("key"), s.currentVersion, true)
	require.NoError(t, err)
	require.False(t, found)

	bytes, entries := s.getSizeInfo()
	require.Equal(t, uint64(1), entries)
	// 3 + 100 = 103
	require.Equal(t, uint64(103), bytes)
}

func TestOverheadTriggersEarlierEviction(t *testing.T) {
	const overhead = 50
	store := map[string][]byte{
		"a": []byte("1234"),
		"b": []byte("5678"),
	}
	read := Reader(func(key []byte) ([]byte, bool, error) {
		v, ok := store[string(key)]
		return v, ok, nil
	})
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = overhead
	s, _ := NewShard(context.Background(), config, read, threading.NewAdHocPool(), 100)

	// "a" + "1234" + 50 = 55 bytes
	s.Get([]byte("a"), s.currentVersion, true)
	_, entries := s.getSizeInfo()
	require.Equal(t, uint64(1), entries)

	// "b" + "5678" + 50 = 55 bytes, total = 110 > 100 → evict "a"
	s.Get([]byte("b"), s.currentVersion, true)
	bytes, entries := s.getSizeInfo()
	require.Equal(t, uint64(1), entries, "overhead should cause eviction to keep only one entry")
	require.LessOrEqual(t, bytes, uint64(100))
}

func TestOverheadIncludedInBatchGetFromDB(t *testing.T) {
	const overhead = 100
	store := map[string][]byte{"x": []byte("10"), "y": []byte("20")}
	read := Reader(func(key []byte) ([]byte, bool, error) {
		v, ok := store[string(key)]
		return v, ok, nil
	})
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = overhead
	s, _ := NewShard(context.Background(), config, read, threading.NewAdHocPool(), 100_000)

	keys := map[string]types.BatchGetResult{"x": {}, "y": {}}
	require.NoError(t, s.BatchGet(keys, s.currentVersion))

	time.Sleep(50 * time.Millisecond)

	bytes, entries := s.getSizeInfo()
	require.Equal(t, uint64(2), entries)
	// (1+2+100) + (1+2+100) = 206
	require.Equal(t, uint64(206), bytes)
}

func TestOverheadSizeUpdatedOnOverwrite(t *testing.T) {
	const overhead = 100
	store := map[string][]byte{"k": []byte("short")}
	read := Reader(func(key []byte) ([]byte, bool, error) {
		v, ok := store[string(key)]
		return v, ok, nil
	})
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = overhead
	s, _ := NewShard(context.Background(), config, read, threading.NewAdHocPool(), 100_000)

	s.Get([]byte("k"), s.currentVersion, true)
	b1, _ := s.getSizeInfo()
	// 1 + 5 + 100 = 106
	require.Equal(t, uint64(106), b1)

	s.lock.Lock()
	s.setInDBCacheUnlocked([]byte("k"), []byte("a-longer-value"))
	s.lock.Unlock()
	b2, entries := s.getSizeInfo()
	require.Equal(t, uint64(1), entries)
	// 1 + 14 + 100 = 115
	require.Equal(t, uint64(115), b2)
}

// ---------------------------------------------------------------------------
// injectValue — edge cases
// ---------------------------------------------------------------------------

func TestInjectValueNotFound(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	val, found, err := s.Get([]byte("missing"), s.currentVersion, true)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)

	s.lock.Lock()
	entry, ok := s.dbCache["missing"]
	s.lock.Unlock()
	require.True(t, ok, "entry should exist in map")
	require.Equal(t, statusDeleted, entry.status)
}

// ---------------------------------------------------------------------------
// Concurrent Set and Get
// ---------------------------------------------------------------------------

func TestConcurrentSetAndGet(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	const n = 100
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(2)
		key := []byte(fmt.Sprintf("key-%d", i))
		val := []byte(fmt.Sprintf("val-%d", i))

		go func() {
			defer wg.Done()
			s.Set(key, val)
		}()
		go func() {
			defer wg.Done()
			s.Get(key, s.currentVersion, true)
		}()
	}

	wg.Wait()
}

func TestConcurrentBatchSetAndBatchGet(t *testing.T) {
	store := map[string][]byte{}
	for i := 0; i < 50; i++ {
		store[fmt.Sprintf("db-%d", i)] = []byte(fmt.Sprintf("v-%d", i))
	}
	s := newTestShard(t, 100_000, store)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		updates := make([]CacheUpdate, 20)
		for i := 0; i < 20; i++ {
			updates[i] = CacheUpdate{
				Key:   []byte(fmt.Sprintf("set-%d", i)),
				Value: []byte(fmt.Sprintf("sv-%d", i)),
			}
		}
		s.BatchSet(updates)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		keys := make(map[string]types.BatchGetResult)
		for i := 0; i < 50; i++ {
			keys[fmt.Sprintf("db-%d", i)] = types.BatchGetResult{}
		}
		s.BatchGet(keys, s.currentVersion)
	}()

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Pool submission failure
// ---------------------------------------------------------------------------

type failPool struct{}

func (fp *failPool) Submit(_ context.Context, _ func()) error {
	return errors.New("pool exhausted")
}

func TestGetPoolSubmitFailure(t *testing.T) {
	readFunc := Reader(func(key []byte) ([]byte, bool, error) { return []byte("v"), true, nil })
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = 0
	s, _ := NewShard(context.Background(), config, readFunc, &failPool{}, 4096)

	_, _, err := s.Get([]byte("k"), s.currentVersion, true)
	require.Error(t, err)
}

func TestBatchGetPoolSubmitFailure(t *testing.T) {
	readFunc := Reader(func(key []byte) ([]byte, bool, error) { return []byte("v"), true, nil })
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = 0
	s, _ := NewShard(context.Background(), config, readFunc, &failPool{}, 4096)

	keys := map[string]types.BatchGetResult{"k": {}}
	err := s.BatchGet(keys, s.currentVersion)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Large values
// ---------------------------------------------------------------------------

func TestSetLargeValueExceedingMaxSizeEvictsOldEntries(t *testing.T) {
	bigVal := make([]byte, 95)
	for i := range bigVal {
		bigVal[i] = 'X'
	}
	store := map[string][]byte{
		"a": []byte("small"),
		"b": bigVal,
	}
	s := newTestShard(t, 100, store)

	s.Get([]byte("a"), s.currentVersion, true)
	s.Get([]byte("b"), s.currentVersion, true)

	bytes, _ := s.getSizeInfo()
	require.LessOrEqual(t, bytes, uint64(100), "size should not exceed maxSize after large set")
}

// ---------------------------------------------------------------------------
// bulkInjectValues — error entries are not cached
// ---------------------------------------------------------------------------

func TestBatchGetDBErrorNotCached(t *testing.T) {
	var calls atomic.Int64
	readFunc := Reader(func(key []byte) ([]byte, bool, error) {
		n := calls.Add(1)
		if n == 1 {
			return nil, false, errors.New("transient db error")
		}
		return []byte("ok"), true, nil
	})
	config := DefaultTestCacheConfig()
	config.EstimatedOverheadPerEntry = 0
	s, _ := NewShard(context.Background(), config, readFunc, threading.NewAdHocPool(), 4096)

	keys := map[string]types.BatchGetResult{"k": {}}
	s.BatchGet(keys, s.currentVersion)

	time.Sleep(50 * time.Millisecond)

	val, found, err := s.Get([]byte("k"), s.currentVersion, true)
	require.NoError(t, err, "retry should succeed")
	require.True(t, found)
	require.Equal(t, "ok", string(val))
}

// ---------------------------------------------------------------------------
// Edge: Set then Delete then BatchGet
// ---------------------------------------------------------------------------

func TestSetDeleteThenBatchGet(t *testing.T) {
	s := newTestShard(t, 4096, map[string][]byte{})

	s.Set([]byte("k"), []byte("v"))
	s.Delete([]byte("k"))

	keys := map[string]types.BatchGetResult{"k": {}}
	require.NoError(t, s.BatchGet(keys, s.currentVersion))
	require.False(t, keys["k"].IsFound())
}
