package unflushed

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util/test"
	"github.com/stretchr/testify/require"
)

func newTestCache(t *testing.T) *UnflushedDataCache {
	t.Helper()
	logger := test.GetLogger()
	ctx := context.Background()
	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)
	return NewUnflushedDataCache(logger, errorMonitor, 64, nil, "test")
}

func makeScopedKeys(keyStrings ...string) []types.ScopedKey {
	keys := make([]types.ScopedKey, len(keyStrings))
	for i, k := range keyStrings {
		keys[i] = types.ScopedKey{Key: []byte(k)}
	}
	return keys
}

func TestGetAfterPut(t *testing.T) {
	cache := newTestCache(t)
	defer cache.Stop()

	cache.PutBatch([]*types.PutRequest{
		{Key: []byte("a"), Value: []byte("val-a")},
		{Key: []byte("b"), Value: []byte("val-b")},
	})

	val, ok := cache.Get([]byte("a"))
	require.True(t, ok)
	require.Equal(t, []byte("val-a"), val)

	val, ok = cache.Get([]byte("b"))
	require.True(t, ok)
	require.Equal(t, []byte("val-b"), val)

	_, ok = cache.Get([]byte("missing"))
	require.False(t, ok)
}

func TestSecondaryKeysAreCached(t *testing.T) {
	cache := newTestCache(t)
	defer cache.Stop()

	cache.PutBatch([]*types.PutRequest{
		{
			Key:   []byte("primary"),
			Value: []byte("hello world"),
			SecondaryKeys: []*types.SecondaryKey{
				{Key: []byte("secondary"), Offset: 0, Length: 5},
			},
		},
	})

	val, ok := cache.Get([]byte("primary"))
	require.True(t, ok)
	require.Equal(t, []byte("hello world"), val)

	val, ok = cache.Get([]byte("secondary"))
	require.True(t, ok)
	require.Equal(t, []byte("hello"), val)
}

func TestEvictionAfterBothReports(t *testing.T) {
	cache := newTestCache(t)

	cache.PutBatch([]*types.PutRequest{
		{Key: []byte("x"), Value: []byte("val-x")},
		{Key: []byte("y"), Value: []byte("val-y")},
	})

	keys := makeScopedKeys("x", "y")
	require.NoError(t, cache.ReportFlushedKeys(keys))
	require.NoError(t, cache.ReportFlushedSegment(keys))
	require.NoError(t, cache.Stop())

	_, ok := cache.Get([]byte("x"))
	require.False(t, ok)
	_, ok = cache.Get([]byte("y"))
	require.False(t, ok)
}

func TestEvictionKeysBeforeSegment(t *testing.T) {
	cache := newTestCache(t)

	cache.PutBatch([]*types.PutRequest{
		{Key: []byte("a"), Value: []byte("1")},
		{Key: []byte("b"), Value: []byte("2")},
	})

	keys := makeScopedKeys("a", "b")
	require.NoError(t, cache.ReportFlushedKeys(keys))
	require.NoError(t, cache.ReportFlushedSegment(keys))
	require.NoError(t, cache.Stop())

	_, ok := cache.Get([]byte("a"))
	require.False(t, ok)
	_, ok = cache.Get([]byte("b"))
	require.False(t, ok)
}

func TestEvictionSegmentBeforeKeys(t *testing.T) {
	cache := newTestCache(t)

	cache.PutBatch([]*types.PutRequest{
		{Key: []byte("a"), Value: []byte("1")},
		{Key: []byte("b"), Value: []byte("2")},
	})

	keys := makeScopedKeys("a", "b")
	require.NoError(t, cache.ReportFlushedSegment(keys))
	require.NoError(t, cache.ReportFlushedKeys(keys))
	require.NoError(t, cache.Stop())

	_, ok := cache.Get([]byte("a"))
	require.False(t, ok)
	_, ok = cache.Get([]byte("b"))
	require.False(t, ok)
}

func TestPartialEviction(t *testing.T) {
	cache := newTestCache(t)

	cache.PutBatch([]*types.PutRequest{
		{Key: []byte("a"), Value: []byte("1")},
	})
	cache.PutBatch([]*types.PutRequest{
		{Key: []byte("b"), Value: []byte("2")},
	})

	keys1 := makeScopedKeys("a")
	keys2 := makeScopedKeys("b")

	// Report both key-flush and segment-flush for "a", but only key-flush for "b".
	require.NoError(t, cache.ReportFlushedKeys(keys1))
	require.NoError(t, cache.ReportFlushedSegment(keys1))
	require.NoError(t, cache.ReportFlushedKeys(keys2))
	require.NoError(t, cache.Stop())

	_, ok := cache.Get([]byte("a"))
	require.False(t, ok)

	val, ok := cache.Get([]byte("b"))
	require.True(t, ok)
	require.Equal(t, []byte("2"), val)
}

func TestMultipleSegments(t *testing.T) {
	cache := newTestCache(t)

	cache.PutBatch([]*types.PutRequest{{Key: []byte("a"), Value: []byte("1")}})
	cache.PutBatch([]*types.PutRequest{{Key: []byte("b"), Value: []byte("2")}})

	keys1 := makeScopedKeys("a")
	keys2 := makeScopedKeys("b")

	require.NoError(t, cache.ReportFlushedKeys(keys1))
	require.NoError(t, cache.ReportFlushedKeys(keys2))
	require.NoError(t, cache.ReportFlushedSegment(keys1))
	require.NoError(t, cache.ReportFlushedSegment(keys2))
	require.NoError(t, cache.Stop())

	_, ok := cache.Get([]byte("a"))
	require.False(t, ok)
	_, ok = cache.Get([]byte("b"))
	require.False(t, ok)
}

func TestStopWithNoWork(t *testing.T) {
	cache := newTestCache(t)
	require.NoError(t, cache.Stop())
}

func TestGetOnEmptyCache(t *testing.T) {
	cache := newTestCache(t)
	defer cache.Stop()

	_, ok := cache.Get([]byte("anything"))
	require.False(t, ok)
}
