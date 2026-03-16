package dbcache

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// ---------------------------------------------------------------------------
// mock batch
// ---------------------------------------------------------------------------

type mockBatch struct {
	sets       []CacheUpdate
	deletes    [][]byte
	committed  bool
	closed     bool
	resetCount int
	commitErr  error
}

func (m *mockBatch) Set(key, value []byte) error {
	m.sets = append(m.sets, CacheUpdate{Key: key, Value: value})
	return nil
}

func (m *mockBatch) Delete(key []byte) error {
	m.deletes = append(m.deletes, key)
	return nil
}

func (m *mockBatch) Commit(opts types.WriteOptions) error {
	if m.commitErr != nil {
		return m.commitErr
	}
	m.committed = true
	return nil
}

func (m *mockBatch) Len() int {
	return len(m.sets) + len(m.deletes)
}

func (m *mockBatch) Reset() {
	m.sets = nil
	m.deletes = nil
	m.committed = false
	m.resetCount++
}

func (m *mockBatch) Close() error {
	m.closed = true
	return nil
}

// ---------------------------------------------------------------------------
// mock cache
// ---------------------------------------------------------------------------

type mockCache struct {
	data        map[string][]byte
	batchSetErr error
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string][]byte)}
}

func (mc *mockCache) Get(_ Reader, key []byte, _ bool) ([]byte, bool, error) {
	v, ok := mc.data[string(key)]
	return v, ok, nil
}

func (mc *mockCache) BatchGet(_ Reader, keys map[string]types.BatchGetResult) error {
	for k := range keys {
		v, ok := mc.data[k]
		if ok {
			keys[k] = types.BatchGetResult{Value: v}
		}
	}
	return nil
}

func (mc *mockCache) Set(key, value []byte) {
	mc.data[string(key)] = value
}

func (mc *mockCache) Delete(key []byte) {
	delete(mc.data, string(key))
}

func (mc *mockCache) BatchSet(updates []CacheUpdate) error {
	if mc.batchSetErr != nil {
		return mc.batchSetErr
	}
	for _, u := range updates {
		if u.IsDelete() {
			delete(mc.data, string(u.Key))
		} else {
			mc.data[string(u.Key)] = u.Value
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestCachedBatchCommitUpdatesCacheOnSuccess(t *testing.T) {
	inner := &mockBatch{}
	cache := newMockCache()
	cb := newCachedBatch(inner, cache)

	require.NoError(t, cb.Set([]byte("a"), []byte("1")))
	require.NoError(t, cb.Set([]byte("b"), []byte("2")))
	require.NoError(t, cb.Commit(types.WriteOptions{}))

	require.True(t, inner.committed)
	v, ok := cache.data["a"]
	require.True(t, ok)
	require.Equal(t, []byte("1"), v)
	v, ok = cache.data["b"]
	require.True(t, ok)
	require.Equal(t, []byte("2"), v)
}

func TestCachedBatchCommitDoesNotUpdateCacheOnInnerFailure(t *testing.T) {
	inner := &mockBatch{commitErr: errors.New("disk full")}
	cache := newMockCache()
	cb := newCachedBatch(inner, cache)

	require.NoError(t, cb.Set([]byte("a"), []byte("1")))
	err := cb.Commit(types.WriteOptions{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "disk full")
	_, ok := cache.data["a"]
	require.False(t, ok, "cache should not be updated when inner commit fails")
}

func TestCachedBatchCommitReturnsCacheError(t *testing.T) {
	inner := &mockBatch{}
	cache := newMockCache()
	cache.batchSetErr = errors.New("cache broken")
	cb := newCachedBatch(inner, cache)

	require.NoError(t, cb.Set([]byte("a"), []byte("1")))
	err := cb.Commit(types.WriteOptions{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "cache broken")
	require.True(t, inner.committed, "inner batch should have committed")
}

func TestCachedBatchDeleteMarksKeyForRemoval(t *testing.T) {
	inner := &mockBatch{}
	cache := newMockCache()
	cache.Set([]byte("x"), []byte("old"))
	cb := newCachedBatch(inner, cache)

	require.NoError(t, cb.Delete([]byte("x")))
	require.NoError(t, cb.Commit(types.WriteOptions{}))

	_, ok := cache.data["x"]
	require.False(t, ok, "key should be deleted from cache")
}

func TestCachedBatchResetClearsPending(t *testing.T) {
	inner := &mockBatch{}
	cache := newMockCache()
	cb := newCachedBatch(inner, cache)

	require.NoError(t, cb.Set([]byte("a"), []byte("1")))
	require.NoError(t, cb.Set([]byte("b"), []byte("2")))
	cb.Reset()

	require.NoError(t, cb.Commit(types.WriteOptions{}))

	require.Empty(t, cache.data, "cache should have no entries after reset + commit")
}

func TestCachedBatchLenDelegatesToInner(t *testing.T) {
	inner := &mockBatch{}
	cache := newMockCache()
	cb := newCachedBatch(inner, cache)

	require.Equal(t, 0, cb.Len())
	require.NoError(t, cb.Set([]byte("a"), []byte("1")))
	require.NoError(t, cb.Delete([]byte("b")))
	require.Equal(t, 2, cb.Len())
}

func TestCachedBatchCloseDelegatesToInner(t *testing.T) {
	inner := &mockBatch{}
	cache := newMockCache()
	cb := newCachedBatch(inner, cache)

	require.NoError(t, cb.Close())
	require.True(t, inner.closed)
}
