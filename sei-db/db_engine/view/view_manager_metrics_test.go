package view

import (
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// TestShardCountsAreConsumedWhenPublished pins that publishing takes every shard's counts, so a count
// is published once and none is left behind for the next scrape to publish again.
func TestShardCountsAreConsumedWhenPublished(t *testing.T) {
	// An hour between scrapes keeps the collect loop from publishing while the test does.
	cm := newViewManagerMetrics(t.Context(), "test-publish", time.Hour,
		func() (uint64, uint64) { return 0, 0 }, 2)

	cm.shard(0).reportCacheHits(3)
	cm.shard(1).reportCacheHits(4)
	cm.shard(1).reportCacheMisses(2)

	cm.publishCounts(t.Context())
	for i := range cm.shards {
		require.Zero(t, cm.shard(i).hits.Load(), "shard %d's hits were not consumed", i)
		require.Zero(t, cm.shard(i).misses.Load(), "shard %d's misses were not consumed", i)
	}
}

// TestShardMetricsFillACacheLine pins the padding that keeps shards counting concurrently off each
// other's cache lines.
func TestShardMetricsFillACacheLine(t *testing.T) {
	require.Equal(t, uintptr(cacheLineBytes), unsafe.Sizeof(shardMetrics{}))
}

// TestNilShardMetricsRecordNothing pins that a shard whose manager has metrics disabled can report
// unconditionally.
func TestNilShardMetricsRecordNothing(t *testing.T) {
	var m *shardMetrics
	require.NotPanics(t, func() {
		m.reportCacheHits(1)
		m.reportCacheMisses(1)
		m.reportCacheMissLatency(time.Millisecond)
	})
}
