package dbcache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type testDB struct {
	mu       sync.RWMutex
	store    map[string][]byte
	getCalls atomic.Int64
	getErr   error
}

func newTestDB(store map[string][]byte) *testDB {
	m := make(map[string][]byte, len(store))
	for k, v := range store {
		m[k] = v
	}
	return &testDB{store: m}
}

func (d *testDB) Get(key []byte) ([]byte, error) {
	d.getCalls.Add(1)
	if d.getErr != nil {
		return nil, d.getErr
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	v, ok := d.store[string(key)]
	if !ok {
		return nil, errorutils.ErrNotFound
	}
	return v, nil
}

func (d *testDB) BatchGet(keys map[string]types.BatchGetResult) error {
	for k := range keys {
		v, err := d.Get([]byte(k))
		if err != nil {
			if errors.Is(err, errorutils.ErrNotFound) {
				keys[k] = types.BatchGetResult{}
			} else {
				keys[k] = types.BatchGetResult{Error: err}
			}
		} else {
			keys[k] = types.BatchGetResult{Value: v}
		}
	}
	return nil
}

func (d *testDB) Set(key, value []byte, opts types.WriteOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.store[string(key)] = value
	return nil
}

func (d *testDB) Delete(key []byte, opts types.WriteOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.store, string(key))
	return nil
}

func (d *testDB) NewIter(opts *types.IterOptions) (types.KeyValueDBIterator, error) {
	panic("not implemented")
}

func (d *testDB) NewBatch() types.Batch {
	return &testBatch{db: d}
}

func (d *testDB) Flush() error { return nil }
func (d *testDB) Close() error { return nil }

type testBatchOp struct {
	key    []byte
	value  []byte
	delete bool
}

type testBatch struct {
	db  *testDB
	ops []testBatchOp
}

func (b *testBatch) Set(key, value []byte) error {
	b.ops = append(b.ops, testBatchOp{
		key:   append([]byte{}, key...),
		value: append([]byte{}, value...),
	})
	return nil
}

func (b *testBatch) Delete(key []byte) error {
	b.ops = append(b.ops, testBatchOp{
		key:    append([]byte{}, key...),
		delete: true,
	})
	return nil
}

func (b *testBatch) Commit(opts types.WriteOptions) error {
	b.db.mu.Lock()
	defer b.db.mu.Unlock()
	for _, op := range b.ops {
		if op.delete {
			delete(b.db.store, string(op.key))
		} else {
			b.db.store[string(op.key)] = op.value
		}
	}
	b.ops = nil
	return nil
}

func (b *testBatch) Len() int     { return len(b.ops) }
func (b *testBatch) Reset()       { b.ops = nil }
func (b *testBatch) Close() error { return nil }

func newTestCache(t *testing.T, store map[string][]byte, shardCount, maxSize uint64) Cache {
	t.Helper()
	config := DefaultTestCacheConfig()
	config.ShardCount = shardCount
	config.MaxSize = maxSize
	config.EstimatedOverheadPerEntry = 1
	config.MetricsName = "test"
	pool := threading.NewAdHocPool()
	db := newTestDB(store)
	c, err := NewStandardCache(context.Background(), config, db, pool, pool)
	require.NoError(t, err)
	return c
}

// ---------------------------------------------------------------------------
// NewStandardCache — validation
// ---------------------------------------------------------------------------

func TestNewStandardCacheValid(t *testing.T) {
	pool := threading.NewAdHocPool()
	config := DefaultTestCacheConfig()
	config.MetricsName = "test"
	config.ShardCount = 4
	config.MaxSize = 1024
	c, err := NewStandardCache(context.Background(), config, newTestDB(nil), pool, pool)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewStandardCacheSingleShard(t *testing.T) {
	pool := threading.NewAdHocPool()
	config := DefaultTestCacheConfig()
	config.MetricsName = "test"
	config.ShardCount = 1
	config.MaxSize = 1024
	c, err := NewStandardCache(context.Background(), config, newTestDB(nil), pool, pool)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewStandardCacheShardCountZero(t *testing.T) {
	pool := threading.NewAdHocPool()
	config := DefaultTestCacheConfig()
	config.MetricsName = "test"
	config.ShardCount = 0
	_, err := NewStandardCache(context.Background(), config, newTestDB(nil), pool, pool)
	require.Error(t, err)
}

func TestNewStandardCacheShardCountNotPowerOfTwo(t *testing.T) {
	pool := threading.NewAdHocPool()
	for _, n := range []uint64{3, 5, 6, 7, 9, 10} {
		config := DefaultTestCacheConfig()
		config.MetricsName = "test"
		config.ShardCount = n
		config.MaxSize = 1024
		_, err := NewStandardCache(context.Background(), config, newTestDB(nil), pool, pool)
		require.Error(t, err, "shardCount=%d", n)
	}
}

func TestNewStandardCacheMaxSizeZero(t *testing.T) {
	pool := threading.NewAdHocPool()
	config := DefaultTestCacheConfig()
	config.MetricsName = "test"
	config.MaxSize = 0
	_, err := NewStandardCache(context.Background(), config, newTestDB(nil), pool, pool)
	require.Error(t, err)
}

func TestNewStandardCacheMaxSizeLessThanShardCount(t *testing.T) {
	pool := threading.NewAdHocPool()
	config := DefaultTestCacheConfig()
	config.MetricsName = "test"
	// shardCount=4, maxSize=3 → MaxSize < ShardCount
	config.ShardCount = 4
	config.MaxSize = 3
	_, err := NewStandardCache(context.Background(), config, newTestDB(nil), pool, pool)
	require.Error(t, err)
}

func TestNewStandardCacheWithMetrics(t *testing.T) {
	pool := threading.NewAdHocPool()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	config := DefaultTestCacheConfig()
	config.MetricsName = "test-cache"
	config.MetricsEnabled = true
	config.MetricsScrapeIntervalSeconds = 3600
	c, err := NewStandardCache(ctx, config, newTestDB(nil), pool, pool)
	require.NoError(t, err)
	require.NotNil(t, c)
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func TestCacheGetFromDB(t *testing.T) {
	store := map[string][]byte{"foo": []byte("bar")}
	c := newTestCache(t, store, 4, 4096)

	val, found, err := c.Get([]byte("foo"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "bar", string(val))
}

func TestCacheGetNotFound(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	val, found, err := c.Get([]byte("missing"), true)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

func TestCacheGetAfterSet(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("k"), []byte("v"))

	val, found, err := c.Get([]byte("k"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v", string(val))
}

func TestCacheGetAfterDelete(t *testing.T) {
	store := map[string][]byte{"k": []byte("v")}
	c := newTestCache(t, store, 4, 4096)

	// Warm the cache so the key is present before deleting.
	_, _, err := c.Get([]byte("k"), true)
	require.NoError(t, err)

	c.Delete([]byte("k"))

	val, found, err := c.Get([]byte("k"), true)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

func TestCacheGetDBError(t *testing.T) {
	dbErr := errors.New("db fail")
	db := newTestDB(nil)
	db.getErr = dbErr
	pool := threading.NewAdHocPool()
	config := DefaultTestCacheConfig()
	config.MetricsName = "test"
	config.ShardCount = 1
	config.MaxSize = 4096
	c, err := NewStandardCache(context.Background(), config, db, pool, pool)
	require.NoError(t, err)

	_, _, err = c.Get([]byte("k"), true)
	require.Error(t, err)
	require.ErrorIs(t, err, dbErr)
}

func TestCacheGetSameKeyConsistentShard(t *testing.T) {
	store := map[string][]byte{"key": []byte("val")}
	db := newTestDB(store)
	pool := threading.NewAdHocPool()
	config := DefaultTestCacheConfig()
	config.MetricsName = "test"
	config.ShardCount = 4
	config.MaxSize = 4096
	c, err := NewStandardCache(context.Background(), config, db, pool, pool)
	require.NoError(t, err)

	val1, _, _ := c.Get([]byte("key"), true)
	val2, _, _ := c.Get([]byte("key"), true)

	require.Equal(t, string(val1), string(val2))
	require.Equal(t, int64(1), db.getCalls.Load(), "second Get should hit cache")
}

// ---------------------------------------------------------------------------
// Set
// ---------------------------------------------------------------------------

func TestCacheSetNewKey(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("a"), []byte("1"))

	val, found, err := c.Get([]byte("a"), false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "1", string(val))
}

func TestCacheSetOverwrite(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("a"), []byte("old"))
	c.Set([]byte("a"), []byte("new"))

	val, found, err := c.Get([]byte("a"), false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "new", string(val))
}

func TestCacheSetNilValue(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("k"), nil)

	val, found, err := c.Get([]byte("k"), false)
	require.NoError(t, err)
	require.False(t, found, "Set(key, nil) should be treated as a deletion")
	require.Nil(t, val)
}

func TestCacheSetNilConsistentWithBatchSet(t *testing.T) {
	store := map[string][]byte{"a": []byte("orig-a"), "b": []byte("orig-b")}

	cSet := newTestCache(t, store, 1, 4096)
	cBatch := newTestCache(t, store, 1, 4096)

	// Warm both caches so the backing store value is loaded.
	_, _, err := cSet.Get([]byte("a"), true)
	require.NoError(t, err)
	_, _, err = cBatch.Get([]byte("b"), true)
	require.NoError(t, err)

	// Delete via Set(key, nil) in one cache and BatchSet({key, nil}) in the other.
	cSet.Set([]byte("a"), nil)
	require.NoError(t, cBatch.BatchSet([]CacheUpdate{
		{Key: []byte("b"), Value: nil},
	}))

	valA, foundA, err := cSet.Get([]byte("a"), false)
	require.NoError(t, err)
	valB, foundB, err := cBatch.Get([]byte("b"), false)
	require.NoError(t, err)

	require.Equal(t, foundA, foundB, "Set(key, nil) and BatchSet with nil value should agree on found")
	require.Equal(t, valA, valB, "Set(key, nil) and BatchSet with nil value should agree on value")
	require.False(t, foundA, "nil value should be treated as a deletion")
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestCacheDeleteExistingKey(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("k"), []byte("v"))
	c.Delete([]byte("k"))

	_, found, err := c.Get([]byte("k"), false)
	require.NoError(t, err)
	require.False(t, found)
}

func TestCacheDeleteNonexistent(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Delete([]byte("ghost"))

	_, found, err := c.Get([]byte("ghost"), false)
	require.NoError(t, err)
	require.False(t, found)
}

func TestCacheDeleteThenSet(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("k"), []byte("v1"))
	c.Delete([]byte("k"))
	c.Set([]byte("k"), []byte("v2"))

	val, found, err := c.Get([]byte("k"), false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v2", string(val))
}

// ---------------------------------------------------------------------------
// BatchSet
// ---------------------------------------------------------------------------

func TestCacheBatchSetMultipleKeys(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	err := c.BatchSet([]CacheUpdate{
		{Key: []byte("a"), Value: []byte("1")},
		{Key: []byte("b"), Value: []byte("2")},
		{Key: []byte("c"), Value: []byte("3")},
	})
	require.NoError(t, err)

	for _, tc := range []struct{ key, want string }{{"a", "1"}, {"b", "2"}, {"c", "3"}} {
		val, found, err := c.Get([]byte(tc.key), false)
		require.NoError(t, err, "key=%q", tc.key)
		require.True(t, found, "key=%q", tc.key)
		require.Equal(t, tc.want, string(val), "key=%q", tc.key)
	}
}

func TestCacheBatchSetMixedSetAndDelete(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("keep"), []byte("v"))
	c.Set([]byte("remove"), []byte("v"))

	err := c.BatchSet([]CacheUpdate{
		{Key: []byte("keep"), Value: []byte("updated")},
		{Key: []byte("remove"), Value: nil},
		{Key: []byte("new"), Value: []byte("fresh")},
	})
	require.NoError(t, err)

	val, found, _ := c.Get([]byte("keep"), false)
	require.True(t, found)
	require.Equal(t, "updated", string(val))

	_, found, _ = c.Get([]byte("remove"), false)
	require.False(t, found)

	val, found, _ = c.Get([]byte("new"), false)
	require.True(t, found)
	require.Equal(t, "fresh", string(val))
}

func TestCacheBatchSetEmpty(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	require.NoError(t, c.BatchSet(nil))
	require.NoError(t, c.BatchSet([]CacheUpdate{}))
}

func TestCacheBatchSetPoolFailure(t *testing.T) {
	readPool := threading.NewAdHocPool()
	config := DefaultTestCacheConfig()
	config.MetricsName = "test"
	config.ShardCount = 1
	config.MaxSize = 4096
	c, _ := NewStandardCache(context.Background(), config, newTestDB(nil), readPool, &failPool{})

	err := c.BatchSet([]CacheUpdate{
		{Key: []byte("k"), Value: []byte("v")},
	})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// BatchGet
// ---------------------------------------------------------------------------

func TestCacheBatchGetAllCached(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("a"), []byte("1"))
	c.Set([]byte("b"), []byte("2"))

	keys := map[string]types.BatchGetResult{"a": {}, "b": {}}
	require.NoError(t, c.BatchGet(keys))

	require.True(t, keys["a"].IsFound())
	require.Equal(t, "1", string(keys["a"].Value))
	require.True(t, keys["b"].IsFound())
	require.Equal(t, "2", string(keys["b"].Value))
}

func TestCacheBatchGetAllFromDB(t *testing.T) {
	store := map[string][]byte{"x": []byte("10"), "y": []byte("20")}
	c := newTestCache(t, store, 4, 4096)

	keys := map[string]types.BatchGetResult{"x": {}, "y": {}}
	require.NoError(t, c.BatchGet(keys))

	require.True(t, keys["x"].IsFound())
	require.Equal(t, "10", string(keys["x"].Value))
	require.True(t, keys["y"].IsFound())
	require.Equal(t, "20", string(keys["y"].Value))
}

func TestCacheBatchGetMixedCachedAndDB(t *testing.T) {
	store := map[string][]byte{"db-key": []byte("from-db")}
	c := newTestCache(t, store, 4, 4096)

	c.Set([]byte("cached"), []byte("from-cache"))

	keys := map[string]types.BatchGetResult{"cached": {}, "db-key": {}}
	require.NoError(t, c.BatchGet(keys))

	require.True(t, keys["cached"].IsFound())
	require.Equal(t, "from-cache", string(keys["cached"].Value))
	require.True(t, keys["db-key"].IsFound())
	require.Equal(t, "from-db", string(keys["db-key"].Value))
}

func TestCacheBatchGetNotFoundKeys(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	keys := map[string]types.BatchGetResult{"nope": {}}
	require.NoError(t, c.BatchGet(keys))
	require.False(t, keys["nope"].IsFound())
}

func TestCacheBatchGetDeletedKey(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("k"), []byte("v"))
	c.Delete([]byte("k"))

	keys := map[string]types.BatchGetResult{"k": {}}
	require.NoError(t, c.BatchGet(keys))
	require.False(t, keys["k"].IsFound())
}

func TestCacheBatchGetDBError(t *testing.T) {
	dbErr := errors.New("broken")
	db := newTestDB(nil)
	db.getErr = dbErr
	pool := threading.NewAdHocPool()
	config := DefaultTestCacheConfig()
	config.MetricsName = "test"
	config.ShardCount = 1
	config.MaxSize = 4096
	c, _ := NewStandardCache(context.Background(), config, db, pool, pool)

	keys := map[string]types.BatchGetResult{"fail": {}}
	require.NoError(t, c.BatchGet(keys), "BatchGet itself should not fail")
	require.Error(t, keys["fail"].Error)
}

func TestCacheBatchGetEmpty(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)
	keys := map[string]types.BatchGetResult{}
	require.NoError(t, c.BatchGet(keys))
}

func TestCacheBatchGetPoolFailure(t *testing.T) {
	readPool := threading.NewAdHocPool()
	config := DefaultTestCacheConfig()
	config.MetricsName = "test"
	config.ShardCount = 1
	config.MaxSize = 4096
	c, _ := NewStandardCache(context.Background(), config, newTestDB(nil), readPool, &failPool{})

	keys := map[string]types.BatchGetResult{"k": {}}
	err := c.BatchGet(keys)
	require.Error(t, err)
}

func TestCacheBatchGetShardReadPoolFailure(t *testing.T) {
	miscPool := threading.NewAdHocPool()
	config := DefaultTestCacheConfig()
	config.MetricsName = "test"
	config.ShardCount = 1
	config.MaxSize = 4096
	c, _ := NewStandardCache(context.Background(), config, newTestDB(nil), &failPool{}, miscPool)

	keys := map[string]types.BatchGetResult{"a": {}, "b": {}}
	require.NoError(t, c.BatchGet(keys))

	for k, r := range keys {
		require.Error(t, r.Error, "key=%q should have per-key error", k)
	}
}

// ---------------------------------------------------------------------------
// Cross-shard distribution
// ---------------------------------------------------------------------------

func TestCacheDistributesAcrossShards(t *testing.T) {
	store := make(map[string][]byte)
	for i := 0; i < 100; i++ {
		store[fmt.Sprintf("key-%d", i)] = []byte("v")
	}
	c := newTestCache(t, store, 4, 4096)
	impl := c.(*cache)

	for i := 0; i < 100; i++ {
		_, _, err := c.Get([]byte(fmt.Sprintf("key-%d", i)), true)
		require.NoError(t, err)
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
	store := map[string][]byte{"key": []byte("val")}
	c := newTestCache(t, store, 4, 4096)
	impl := c.(*cache)

	_, _, err := c.Get([]byte("key"), true)
	require.NoError(t, err)

	idx := impl.shardManager.Shard([]byte("key"))
	_, entries := impl.shards[idx].getSizeInfo()
	require.Equal(t, uint64(1), entries, "key should be in the shard determined by shardManager")
}

// ---------------------------------------------------------------------------
// getCacheSizeInfo
// ---------------------------------------------------------------------------

func TestCacheGetCacheSizeInfoEmpty(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)
	impl := c.(*cache)

	bytes, entries := impl.getCacheSizeInfo()
	require.Equal(t, uint64(0), bytes)
	require.Equal(t, uint64(0), entries)
}

func TestCacheGetCacheSizeInfoAggregatesShards(t *testing.T) {
	store := make(map[string][]byte)
	for i := 0; i < 20; i++ {
		store[fmt.Sprintf("k%d", i)] = []byte(fmt.Sprintf("v%d", i))
	}
	c := newTestCache(t, store, 4, 4096)
	impl := c.(*cache)

	for i := 0; i < 20; i++ {
		_, _, err := c.Get([]byte(fmt.Sprintf("k%d", i)), true)
		require.NoError(t, err)
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
	store := map[string][]byte{
		"ab":  []byte("cd"),
		"efg": []byte("hi"),
	}
	pool := threading.NewAdHocPool()
	config := DefaultTestCacheConfig()
	config.MetricsName = "test"
	config.ShardCount = 1
	config.MaxSize = 100_000
	config.EstimatedOverheadPerEntry = overhead
	db := newTestDB(store)
	c, err := NewStandardCache(context.Background(), config, db, pool, pool)
	require.NoError(t, err)
	impl := c.(*cache)

	_, _, err = c.Get([]byte("ab"), true)
	require.NoError(t, err)
	_, _, err = c.Get([]byte("efg"), true)
	require.NoError(t, err)

	bytes, entries := impl.getCacheSizeInfo()
	require.Equal(t, uint64(2), entries)
	// (2+2+200) + (3+2+200) = 409
	require.Equal(t, uint64(409), bytes)
}

func TestCacheOverheadCausesEarlierEviction(t *testing.T) {
	const overhead = 200
	store := map[string][]byte{
		"a": []byte("0123456789"),
		"b": []byte("0123456789"),
		"c": []byte("0123456789"),
	}
	pool := threading.NewAdHocPool()
	// Single shard, maxSize=500. Each 10-byte value entry costs 1+10+200=211 bytes.
	// Two entries = 422 < 500. Three entries = 633 > 500, so one must be evicted.
	config := DefaultTestCacheConfig()
	config.MetricsName = "test"
	config.ShardCount = 1
	config.MaxSize = 500
	config.EstimatedOverheadPerEntry = overhead
	db := newTestDB(store)
	c, err := NewStandardCache(context.Background(), config, db, pool, pool)
	require.NoError(t, err)
	impl := c.(*cache)

	_, _, err = c.Get([]byte("a"), true)
	require.NoError(t, err)
	_, _, err = c.Get([]byte("b"), true)
	require.NoError(t, err)

	_, entries := impl.getCacheSizeInfo()
	require.Equal(t, uint64(2), entries, "two entries should fit")

	_, _, err = c.Get([]byte("c"), true)
	require.NoError(t, err)

	bytes, entries := impl.getCacheSizeInfo()
	require.Equal(t, uint64(2), entries, "third entry should trigger eviction")
	require.LessOrEqual(t, bytes, uint64(500))
}

// ---------------------------------------------------------------------------
// Many keys — BatchGet/BatchSet spanning all shards
// ---------------------------------------------------------------------------

func TestCacheBatchSetThenBatchGetManyKeys(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 100_000)

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
	require.NoError(t, c.BatchGet(keys))

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
	c := newTestCache(t, store, 4, 100_000)

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
			c.Get(key, true)
		}()
	}
	wg.Wait()
}

func TestCacheConcurrentBatchSetAndBatchGet(t *testing.T) {
	store := map[string][]byte{}
	for i := 0; i < 50; i++ {
		store[fmt.Sprintf("db-%d", i)] = []byte(fmt.Sprintf("v-%d", i))
	}
	c := newTestCache(t, store, 4, 100_000)

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
		c.BatchGet(keys)
	}()

	wg.Wait()
}

func TestCacheConcurrentDeleteAndGet(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 100_000)

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
			c.Get(key, true)
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Eviction through the cache layer
// ---------------------------------------------------------------------------

func TestCacheEvictsPerShard(t *testing.T) {
	store := map[string][]byte{
		"a": []byte("11111111"),
		"b": []byte("22222222"),
		"c": []byte("33333333"),
	}
	c := newTestCache(t, store, 1, 20)
	impl := c.(*cache)

	_, _, err := c.Get([]byte("a"), true)
	require.NoError(t, err)
	_, _, err = c.Get([]byte("b"), true)
	require.NoError(t, err)

	_, _, err = c.Get([]byte("c"), true)
	require.NoError(t, err)

	bytes, _ := impl.shards[0].getSizeInfo()
	require.LessOrEqual(t, bytes, uint64(20))
}

// ---------------------------------------------------------------------------
// Edge: BatchSet with keys all routed to the same shard
// ---------------------------------------------------------------------------

func TestCacheBatchSetSameShard(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 1, 4096)

	err := c.BatchSet([]CacheUpdate{
		{Key: []byte("x"), Value: []byte("1")},
		{Key: []byte("y"), Value: []byte("2")},
		{Key: []byte("z"), Value: []byte("3")},
	})
	require.NoError(t, err)

	for _, tc := range []struct{ key, want string }{{"x", "1"}, {"y", "2"}, {"z", "3"}} {
		val, found, err := c.Get([]byte(tc.key), false)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, tc.want, string(val))
	}
}

// ---------------------------------------------------------------------------
// Edge: BatchGet after BatchSet with deletes
// ---------------------------------------------------------------------------

func TestCacheBatchGetAfterBatchSetWithDeletes(t *testing.T) {
	c := newTestCache(t, map[string][]byte{}, 4, 4096)

	c.Set([]byte("a"), []byte("1"))
	c.Set([]byte("b"), []byte("2"))
	c.Set([]byte("c"), []byte("3"))

	err := c.BatchSet([]CacheUpdate{
		{Key: []byte("a"), Value: []byte("updated")},
		{Key: []byte("b"), Value: nil},
	})
	require.NoError(t, err)

	keys := map[string]types.BatchGetResult{"a": {}, "b": {}, "c": {}}
	require.NoError(t, c.BatchGet(keys))

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
		config := DefaultTestCacheConfig()
		config.MetricsName = "test"
		config.ShardCount = n
		config.MaxSize = n * 100
		c, err := NewStandardCache(context.Background(), config, newTestDB(nil), pool, pool)
		require.NoError(t, err, "shardCount=%d", n)
		require.NotNil(t, c, "shardCount=%d", n)
	}
}
