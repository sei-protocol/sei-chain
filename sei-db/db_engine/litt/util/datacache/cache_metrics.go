package cache

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"

	sharedmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

// CacheMetrics records OTel metrics for a LittDB FIFO cache. A nil receiver is a no-op,
// allowing callers to record unconditionally regardless of whether metrics are configured.
type CacheMetrics struct {
	keyCount        metric.Int64Gauge
	weight          metric.Int64Gauge
	keysAdded       metric.Int64Counter
	weightAdded     metric.Int64Counter
	evictionLatency metric.Float64Histogram
}

// NewCacheMetrics creates a CacheMetrics that records via the supplied OTel meter.
// cacheName is embedded in the instrument name (e.g. "chunk_write" →
// "litt_chunk_write_cache_key_count"). If meter is nil, returns nil.
func NewCacheMetrics(meter metric.Meter, cacheName string) *CacheMetrics {
	if meter == nil {
		return nil
	}

	keyCount, _ := meter.Int64Gauge(
		"litt_"+cacheName+"_cache_key_count",
		metric.WithDescription("Current number of keys in the cache"),
		metric.WithUnit("{count}"),
	)
	weight, _ := meter.Int64Gauge(
		"litt_"+cacheName+"_cache_weight",
		metric.WithDescription("Current total weight of cache entries"),
		metric.WithUnit("By"),
	)
	keysAdded, _ := meter.Int64Counter(
		"litt_"+cacheName+"_cache_keys_added",
		metric.WithDescription("Number of keys added to the cache"),
		metric.WithUnit("{count}"),
	)
	weightAdded, _ := meter.Int64Counter(
		"litt_"+cacheName+"_cache_weight_added",
		metric.WithDescription("Total weight of entries added to the cache"),
		metric.WithUnit("By"),
	)
	evictionLatency, _ := meter.Float64Histogram(
		"litt_"+cacheName+"_cache_eviction_latency",
		metric.WithDescription("Age of entries at eviction time"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(sharedmetrics.LatencyBuckets...),
	)

	return &CacheMetrics{
		keyCount:        keyCount,
		weight:          weight,
		keysAdded:       keysAdded,
		weightAdded:     weightAdded,
		evictionLatency: evictionLatency,
	}
}

func (m *CacheMetrics) reportInsertion(weight uint64) {
	if m == nil {
		return
	}
	ctx := context.Background()
	m.keysAdded.Add(ctx, 1)
	m.weightAdded.Add(ctx, int64(weight)) //nolint:gosec
}

func (m *CacheMetrics) reportEviction(age time.Duration) {
	if m == nil {
		return
	}
	m.evictionLatency.Record(context.Background(), age.Seconds())
}

func (m *CacheMetrics) reportCurrentSize(size int, weight uint64) {
	if m == nil {
		return
	}
	ctx := context.Background()
	m.keyCount.Record(ctx, int64(size)) //nolint:gosec
	m.weight.Record(ctx, int64(weight)) //nolint:gosec
}
