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

func noopRead(key []byte) ([]byte, bool, error) { return nil, false, nil }

func newTestCache(t *testing.T, store map[string][]byte, shardCount, maxSize uint64) (Cache, Reader) {
	t.Helper()
	read := func(key []byte) ([]byte, bool, error) {
		v, ok := store[string(key)]
		if !ok {
			return nil, false, nil
		}
		return v, true, nil
	}
	pool := threading.NewAdHocPool()
	c, err := NewStandardCache(context.Background(), shardCount, maxSize, pool, pool, 16, "", 0)
	require.NoError(t, err)
	return c, read
}

// ---------------------------------------------------------------------------
// NewStandardCache — validation
// ---------------------------------------------------------------------------

func TestNewStandardCacheValid(t *testing.T) {
	pool := threading.NewAdHocPool()
	c, err := NewStandardCache(context.Background(), 4, 1024, pool, pool, 16, "", 0)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewStandardCacheSingleShard(t *testing.T) {
	pool := threading.NewAdHocPool()
	c, err := NewStandardCache(context.Background(), 1, 1024, pool, pool, 16, "", 0)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewStandardCacheShardCountZero(t *testing.T) {
	pool := threading.NewAdHocPool()
	_, err := NewStandardCache(context.Background(), 0, 1024, pool, pool, 16, "", 0)
	require.Error(t, err)
}

func TestNewStandardCacheShardCountNotPowerOfTwo(t *testing.T) {
	pool := threading.NewAdHocPool()
	for _, n := range []uint64{3, 5, 6, 7, 9, 10} {
		_, err := NewStandardCache(context.Background(), n, 1024, pool, pool, 16, "", 0)
		require.Error(t, err, "shardCount=%d", n)
	}
}

func TestNewStandardCacheMaxSizeZero(t *testing.T) {
	pool := threading.NewAdHocPool()
	_, err := NewStandardCache(context.Background(), 4, 0, pool, pool, 16, "", 0)
	require.Error(t, err)
}

func TestNewStandardCacheMaxSizeLessThanShardCount(t *testing.T) {
	pool := threading.NewAdHocPool()
	// shardCount=4, maxSize=3 → sizePerShard=0
	_, err := NewStandardCache(context.Background(), 4, 3, pool, pool, 16, "", 0)
	require.Error(t, err)
}

func TestNewStandardCacheWithMetrics(t *testing.T) {
	pool := threading.NewAdHocPool()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := NewStandardCache(ctx, 2, 1024, pool, pool, 0, "test-cache", time.Hour)
	require.NoError(t, err)
	require.NotNil(t, c)
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func TestCacheGetFromDB(t *testing.T) {
	store := map[string][]byte{"foo": []byte("bar")}
	c, read := newTestCache(t, store, 4, 4096)

	val, found, err := c.Get(read, []byte("foo"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "bar", string(val))
}

func TestCacheGetNotFound(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	val, found, err := c.Get(read, []byte("missing"), true)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

func TestCacheGetAfterSet(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("k"), []byte("v"))

	val, found, err := c.Get(read, []byte("k"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v", string(val))
}

func TestCacheGetAfterDelete(t *testing.T) {
	store := map[string][]byte{"k": []byte("v")}
	c, read := newTestCache(t, store, 4, 4096)

	// Warm the cache so the key is present before deleting.
	_, _, err := c.Get(read, []byte("k"), true)
	require.NoError(t, err)

	c.Delete([]byte("k"))

	val, found, err := c.Get(read, []byte("k"), true)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

func TestCacheGetDBError(t *testing.T) {
	dbErr := errors.New("db fail")
	readFunc := func(key []byte) ([]byte, bool, error) { return nil, false, dbErr }
	pool := threading.NewAdHocPool()
	c, _ := NewStandardCache(context.Background(), 1, 4096, pool, pool, 0, "", 0)

	_, _, err := c.Get(readFunc, []byte("k"), true)
	require.Error(t, err)
	require.ErrorIs(t, err, dbErr)
}

func TestCacheGetSameKeyConsistentShard(t *testing.T) {
	var readCalls atomic.Int64
	readFunc := func(key []byte) ([]byte, bool, error) {
		readCalls.Add(1)
		return []byte("val"), true, nil
	}
	pool := threading.NewAdHocPool()
	c, _ := NewStandardCache(context.Background(), 4, 4096, pool, pool, 0, "", 0)

	val1, _, _ := c.Get(readFunc, []byte("key"), true)
	val2, _, _ := c.Get(readFunc, []byte("key"), true)

	require.Equal(t, string(val1), string(val2))
	require.Equal(t, int64(1), readCalls.Load(), "second Get should hit cache")
}

// ---------------------------------------------------------------------------
// Set
// ---------------------------------------------------------------------------

func TestCacheSetNewKey(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("a"), []byte("1"))

	val, found, err := c.Get(read, []byte("a"), false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "1", string(val))
}

func TestCacheSetOverwrite(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("a"), []byte("old"))
	c.Set([]byte("a"), []byte("new"))

	val, found, err := c.Get(read, []byte("a"), false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "new", string(val))
}

func TestCacheSetNilValue(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("k"), nil)

	val, found, err := c.Get(read, []byte("k"), false)
	require.NoError(t, err)
	require.False(t, found, "Set(key, nil) should be treated as a deletion")
	require.Nil(t, val)
}

func TestCacheSetNilConsistentWithBatchSet(t *testing.T) {
	store := map[string][]byte{"a": []byte("orig-a"), "b": []byte("orig-b")}

	cSet, readSet := newTestCache(t, store, 1, 4096)
	cBatch, readBatch := newTestCache(t, store, 1, 4096)

	// Warm both caches so the backing store value is loaded.
	_, _, err := cSet.Get(readSet, []byte("a"), true)
	require.NoError(t, err)
	_, _, err = cBatch.Get(readBatch, []byte("b"), true)
	require.NoError(t, err)

	// Delete via Set(key, nil) in one cache and BatchSet({key, nil}) in the other.
	cSet.Set([]byte("a"), nil)
	require.NoError(t, cBatch.BatchSet([]CacheUpdate{
		{Key: []byte("b"), Value: nil},
	}))

	valA, foundA, err := cSet.Get(readSet, []byte("a"), false)
	require.NoError(t, err)
	valB, foundB, err := cBatch.Get(readBatch, []byte("b"), false)
	require.NoError(t, err)

	require.Equal(t, foundA, foundB, "Set(key, nil) and BatchSet with nil value should agree on found")
	require.Equal(t, valA, valB, "Set(key, nil) and BatchSet with nil value should agree on value")
	require.False(t, foundA, "nil value should be treated as a deletion")
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestCacheDeleteExistingKey(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("k"), []byte("v"))
	c.Delete([]byte("k"))

	_, found, err := c.Get(read, []byte("k"), false)
	require.NoError(t, err)
	require.False(t, found)
}

func TestCacheDeleteNonexistent(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Delete([]byte("ghost"))

	_, found, err := c.Get(read, []byte("ghost"), false)
	require.NoError(t, err)
	require.False(t, found)
}

func TestCacheDeleteThenSet(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("k"), []byte("v1"))
	c.Delete([]byte("k"))
	c.Set([]byte("k"), []byte("v2"))

	val, found, err := c.Get(read, []byte("k"), false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v2", string(val))
}

// ---------------------------------------------------------------------------
// BatchSet
// ---------------------------------------------------------------------------

func TestCacheBatchSetMultipleKeys(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	err := c.BatchSet([]CacheUpdate{
		{Key: []byte("a"), Value: []byte("1")},
		{Key: []byte("b"), Value: []byte("2")},
		{Key: []byte("c"), Value: []byte("3")},
	})
	require.NoError(t, err)

	for _, tc := range []struct{ key, want string }{{"a", "1"}, {"b", "2"}, {"c", "3"}} {
		val, found, err := c.Get(read, []byte(tc.key), false)
		require.NoError(t, err, "key=%q", tc.key)
		require.True(t, found, "key=%q", tc.key)
		require.Equal(t, tc.want, string(val), "key=%q", tc.key)
	}
}

func TestCacheBatchSetMixedSetAndDelete(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("keep"), []byte("v"))
	c.Set([]byte("remove"), []byte("v"))

	err := c.BatchSet([]CacheUpdate{
		{Key: []byte("keep"), Value: []byte("updated")},
		{Key: []byte("remove"), Value: nil},
		{Key: []byte("new"), Value: []byte("fresh")},
	})
	require.NoError(t, err)

	val, found, _ := c.Get(read, []byte("keep"), false)
	require.True(t, found)
	require.Equal(t, "updated", string(val))

	_, found, _ = c.Get(read, []byte("remove"), false)
	require.False(t, found)

	val, found, _ = c.Get(read, []byte("new"), false)
	require.True(t, found)
	require.Equal(t, "fresh", string(val))
}

func TestCacheBatchSetEmpty(t *testing.T) {
	c, _ := newTestCache(t, map[string][]byte{}, 4, 4096)

	require.NoError(t, c.BatchSet(nil))
	require.NoError(t, c.BatchSet([]CacheUpdate{}))
}

func TestCacheBatchSetPoolFailure(t *testing.T) {
	readPool := threading.NewAdHocPool()
	c, _ := NewStandardCache(context.Background(), 1, 4096, readPool, &failPool{}, 0, "", 0)

	err := c.BatchSet([]CacheUpdate{
		{Key: []byte("k"), Value: []byte("v")},
	})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// BatchGet
// ---------------------------------------------------------------------------

func TestCacheBatchGetAllCached(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("a"), []byte("1"))
	c.Set([]byte("b"), []byte("2"))

	keys := map[string]types.BatchGetResult{"a": {}, "b": {}}
	require.NoError(t, c.BatchGet(read, keys))

	require.True(t, keys["a"].IsFound())
	require.Equal(t, "1", string(keys["a"].Value))
	require.True(t, keys["b"].IsFound())
	require.Equal(t, "2", string(keys["b"].Value))
}

func TestCacheBatchGetAllFromDB(t *testing.T) {
	store := map[string][]byte{"x": []byte("10"), "y": []byte("20")}
	c, read := newTestCache(t, store, 4, 4096)

	keys := map[string]types.BatchGetResult{"x": {}, "y": {}}
	require.NoError(t, c.BatchGet(read, keys))

	require.True(t, keys["x"].IsFound())
	require.Equal(t, "10", string(keys["x"].Value))
	require.True(t, keys["y"].IsFound())
	require.Equal(t, "20", string(keys["y"].Value))
}

func TestCacheBatchGetMixedCachedAndDB(t *testing.T) {
	store := map[string][]byte{"db-key": []byte("from-db")}
	c, read := newTestCache(t, store, 4, 4096)

	c.Set([]byte("cached"), []byte("from-cache"))

	keys := map[string]types.BatchGetResult{"cached": {}, "db-key": {}}
	require.NoError(t, c.BatchGet(read, keys))

	require.True(t, keys["cached"].IsFound())
	require.Equal(t, "from-cache", string(keys["cached"].Value))
	require.True(t, keys["db-key"].IsFound())
	require.Equal(t, "from-db", string(keys["db-key"].Value))
}

func TestCacheBatchGetNotFoundKeys(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	keys := map[string]types.BatchGetResult{"nope": {}}
	require.NoError(t, c.BatchGet(read, keys))
	require.False(t, keys["nope"].IsFound())
}

func TestCacheBatchGetDeletedKey(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("k"), []byte("v"))
	c.Delete([]byte("k"))

	keys := map[string]types.BatchGetResult{"k": {}}
	require.NoError(t, c.BatchGet(read, keys))
	require.False(t, keys["k"].IsFound())
}

func TestCacheBatchGetDBError(t *testing.T) {
	dbErr := errors.New("broken")
	readFunc := func(key []byte) ([]byte, bool, error) { return nil, false, dbErr }
	pool := threading.NewAdHocPool()
	c, _ := NewStandardCache(context.Background(), 1, 4096, pool, pool, 0, "", 0)

	keys := map[string]types.BatchGetResult{"fail": {}}
	require.NoError(t, c.BatchGet(readFunc, keys), "BatchGet itself should not fail")
	require.Error(t, keys["fail"].Error)
}

func TestCacheBatchGetEmpty(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)
	keys := map[string]types.BatchGetResult{}
	require.NoError(t, c.BatchGet(read, keys))
}

func TestCacheBatchGetPoolFailure(t *testing.T) {
	readPool := threading.NewAdHocPool()
	c, _ := NewStandardCache(context.Background(), 1, 4096, readPool, &failPool{}, 0, "", 0)

	keys := map[string]types.BatchGetResult{"k": {}}
	err := c.BatchGet(noopRead, keys)
	require.Error(t, err)
}

func TestCacheBatchGetShardReadPoolFailure(t *testing.T) {
	miscPool := threading.NewAdHocPool()
	c, _ := NewStandardCache(context.Background(), 1, 4096, &failPool{}, miscPool, 0, "", 0)

	keys := map[string]types.BatchGetResult{"a": {}, "b": {}}
	require.NoError(t, c.BatchGet(noopRead, keys))

	for k, r := range keys {
		require.Error(t, r.Error, "key=%q should have per-key error", k)
	}
}

// ---------------------------------------------------------------------------
// Cross-shard distribution
// ---------------------------------------------------------------------------

func TestCacheDistributesAcrossShards(t *testing.T) {
	c, _ := newTestCache(t, map[string][]byte{}, 4, 4096)
	impl := c.(*cache)

	for i := 0; i < 100; i++ {
		c.Set([]byte(fmt.Sprintf("key-%d", i)), []byte("v"))
	}

	nonEmpty := 0
	for _, s := range impl.shards {
		_, entries := s.getSizeInfo()
		if entries > 0 {
			nonEmpty++
		}
	}
	require.GreaterOrEqual(t, nonEmpty, 2, "keys should distribute across multiple shards")
}

func TestCacheGetRoutesToSameShard(t *testing.T) {
	c, _ := newTestCache(t, map[string][]byte{}, 4, 4096)
	impl := c.(*cache)

	c.Set([]byte("key"), []byte("val"))

	idx := impl.shardManager.Shard([]byte("key"))
	_, entries := impl.shards[idx].getSizeInfo()
	require.Equal(t, uint64(1), entries, "key should be in the shard determined by shardManager")
}

// ---------------------------------------------------------------------------
// getCacheSizeInfo
// ---------------------------------------------------------------------------

func TestCacheGetCacheSizeInfoEmpty(t *testing.T) {
	c, _ := newTestCache(t, map[string][]byte{}, 4, 4096)
	impl := c.(*cache)

	bytes, entries := impl.getCacheSizeInfo()
	require.Equal(t, uint64(0), bytes)
	require.Equal(t, uint64(0), entries)
}

func TestCacheGetCacheSizeInfoAggregatesShards(t *testing.T) {
	c, _ := newTestCache(t, map[string][]byte{}, 4, 4096)
	impl := c.(*cache)

	for i := 0; i < 20; i++ {
		c.Set([]byte(fmt.Sprintf("k%d", i)), []byte(fmt.Sprintf("v%d", i)))
	}

	bytes, entries := impl.getCacheSizeInfo()
	require.Equal(t, uint64(20), entries)
	require.Greater(t, bytes, uint64(0))
}

// ---------------------------------------------------------------------------
// estimatedOverheadPerEntry
// ---------------------------------------------------------------------------

func TestCacheSizeInfoIncludesOverhead(t *testing.T) {
	const overhead = 200
	pool := threading.NewAdHocPool()
	c, err := NewStandardCache(context.Background(), 1, 100_000, pool, pool, overhead, "", 0)
	require.NoError(t, err)
	impl := c.(*cache)

	c.Set([]byte("ab"), []byte("cd"))
	c.Set([]byte("efg"), []byte("hi"))

	bytes, entries := impl.getCacheSizeInfo()
	require.Equal(t, uint64(2), entries)
	// (2+2+200) + (3+2+200) = 409
	require.Equal(t, uint64(409), bytes)
}

func TestCacheOverheadCausesEarlierEviction(t *testing.T) {
	const overhead = 200
	pool := threading.NewAdHocPool()
	// Single shard, maxSize=500. Each 10-byte value entry costs 1+10+200=211 bytes.
	// Two entries = 422 < 500. Three entries = 633 > 500, so one must be evicted.
	c, err := NewStandardCache(context.Background(), 1, 500, pool, pool, overhead, "", 0)
	require.NoError(t, err)
	impl := c.(*cache)

	c.Set([]byte("a"), []byte("0123456789"))
	c.Set([]byte("b"), []byte("0123456789"))

	_, entries := impl.getCacheSizeInfo()
	require.Equal(t, uint64(2), entries, "two entries should fit")

	c.Set([]byte("c"), []byte("0123456789"))

	bytes, entries := impl.getCacheSizeInfo()
	require.Equal(t, uint64(2), entries, "third entry should trigger eviction")
	require.LessOrEqual(t, bytes, uint64(500))
}

// ---------------------------------------------------------------------------
// Many keys — BatchGet/BatchSet spanning all shards
// ---------------------------------------------------------------------------

func TestCacheBatchSetThenBatchGetManyKeys(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 100_000)

	updates := make([]CacheUpdate, 200)
	for i := range updates {
		updates[i] = CacheUpdate{
			Key:   []byte(fmt.Sprintf("key-%03d", i)),
			Value: []byte(fmt.Sprintf("val-%03d", i)),
		}
	}
	require.NoError(t, c.BatchSet(updates))

	keys := make(map[string]types.BatchGetResult, 200)
	for i := 0; i < 200; i++ {
		keys[fmt.Sprintf("key-%03d", i)] = types.BatchGetResult{}
	}
	require.NoError(t, c.BatchGet(read, keys))

	for i := 0; i < 200; i++ {
		k := fmt.Sprintf("key-%03d", i)
		want := fmt.Sprintf("val-%03d", i)
		require.True(t, keys[k].IsFound(), "key=%q", k)
		require.Equal(t, want, string(keys[k].Value), "key=%q", k)
		require.NoError(t, keys[k].Error, "key=%q", k)
	}
}

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

func TestCacheConcurrentGetSet(t *testing.T) {
	store := map[string][]byte{}
	for i := 0; i < 50; i++ {
		store[fmt.Sprintf("db-%d", i)] = []byte(fmt.Sprintf("v-%d", i))
	}
	c, read := newTestCache(t, store, 4, 100_000)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		key := []byte(fmt.Sprintf("key-%d", i))
		val := []byte(fmt.Sprintf("val-%d", i))

		go func() {
			defer wg.Done()
			c.Set(key, val)
		}()
		go func() {
			defer wg.Done()
			c.Get(read, key, true)
		}()
	}
	wg.Wait()
}

func TestCacheConcurrentBatchSetAndBatchGet(t *testing.T) {
	store := map[string][]byte{}
	for i := 0; i < 50; i++ {
		store[fmt.Sprintf("db-%d", i)] = []byte(fmt.Sprintf("v-%d", i))
	}
	c, read := newTestCache(t, store, 4, 100_000)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		updates := make([]CacheUpdate, 50)
		for i := range updates {
			updates[i] = CacheUpdate{
				Key:   []byte(fmt.Sprintf("set-%d", i)),
				Value: []byte(fmt.Sprintf("sv-%d", i)),
			}
		}
		c.BatchSet(updates)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		keys := make(map[string]types.BatchGetResult)
		for i := 0; i < 50; i++ {
			keys[fmt.Sprintf("db-%d", i)] = types.BatchGetResult{}
		}
		c.BatchGet(read, keys)
	}()

	wg.Wait()
}

func TestCacheConcurrentDeleteAndGet(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 100_000)

	for i := 0; i < 100; i++ {
		c.Set([]byte(fmt.Sprintf("k-%d", i)), []byte("v"))
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		key := []byte(fmt.Sprintf("k-%d", i))
		go func() {
			defer wg.Done()
			c.Delete(key)
		}()
		go func() {
			defer wg.Done()
			c.Get(read, key, true)
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Eviction through the cache layer
// ---------------------------------------------------------------------------

func TestCacheEvictsPerShard(t *testing.T) {
	c, _ := newTestCache(t, map[string][]byte{}, 1, 20)
	impl := c.(*cache)

	c.Set([]byte("a"), []byte("11111111"))
	c.Set([]byte("b"), []byte("22222222"))

	c.Set([]byte("c"), []byte("33333333"))

	bytes, _ := impl.shards[0].getSizeInfo()
	require.LessOrEqual(t, bytes, uint64(20))
}

// ---------------------------------------------------------------------------
// Edge: BatchSet with keys all routed to the same shard
// ---------------------------------------------------------------------------

func TestCacheBatchSetSameShard(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 1, 4096)

	err := c.BatchSet([]CacheUpdate{
		{Key: []byte("x"), Value: []byte("1")},
		{Key: []byte("y"), Value: []byte("2")},
		{Key: []byte("z"), Value: []byte("3")},
	})
	require.NoError(t, err)

	for _, tc := range []struct{ key, want string }{{"x", "1"}, {"y", "2"}, {"z", "3"}} {
		val, found, err := c.Get(read, []byte(tc.key), false)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, tc.want, string(val))
	}
}

// ---------------------------------------------------------------------------
// Edge: BatchGet after BatchSet with deletes
// ---------------------------------------------------------------------------

func TestCacheBatchGetAfterBatchSetWithDeletes(t *testing.T) {
	c, read := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("a"), []byte("1"))
	c.Set([]byte("b"), []byte("2"))
	c.Set([]byte("c"), []byte("3"))

	err := c.BatchSet([]CacheUpdate{
		{Key: []byte("a"), Value: []byte("updated")},
		{Key: []byte("b"), Value: nil},
	})
	require.NoError(t, err)

	keys := map[string]types.BatchGetResult{"a": {}, "b": {}, "c": {}}
	require.NoError(t, c.BatchGet(read, keys))

	require.True(t, keys["a"].IsFound())
	require.Equal(t, "updated", string(keys["a"].Value))
	require.False(t, keys["b"].IsFound())
	require.True(t, keys["c"].IsFound())
	require.Equal(t, "3", string(keys["c"].Value))
}

// ---------------------------------------------------------------------------
// Power-of-two shard counts
// ---------------------------------------------------------------------------

func TestNewStandardCachePowerOfTwoShardCounts(t *testing.T) {
	pool := threading.NewAdHocPool()
	for _, n := range []uint64{1, 2, 4, 8, 16, 32, 64} {
		c, err := NewStandardCache(context.Background(), n, n*100, pool, pool, 0, "", 0)
		require.NoError(t, err, "shardCount=%d", n)
		require.NotNil(t, c, "shardCount=%d", n)
	}
}
