package util

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

const cacheMeterName = "litt"

// CacheMetrics is a struct that holds OTel metrics for a cache. A nil
// CacheMetrics instance acts as a no-op for all report* methods.
//
// Multiple CacheMetrics instances may be created for the same process; each
// receives references to the same underlying instruments because OTel
// instrument registration is idempotent. The "cache" attribute (set at
// construction time) distinguishes series in the exporter
// (e.g. litt_chunk_cache_keys_added{cache="chunk_read"}).
type CacheMetrics struct {
	// Pre-computed attribute option reused on every recording to avoid
	// per-call allocations on the hot path.
	attrs metric.MeasurementOption

	keyCount        metric.Int64Gauge
	weight          metric.Int64Gauge
	keysAdded       metric.Int64Counter
	weightAdded     metric.Int64Counter
	evictionLatency metric.Float64Histogram
}

// NewCacheMetrics creates a new CacheMetrics that records via the global OTel
// MeterProvider. The cacheName is attached as the "cache" attribute on every
// observation, allowing multiple cache instances to be distinguished in the
// exporter (for example "chunk_read" vs "chunk_write").
//
// The caller must have configured a MeterProvider before calling this (e.g.
// commonmetrics.SetupOtelPrometheus).
func NewCacheMetrics(cacheName string) *CacheMetrics {
	meter := otel.Meter(cacheMeterName)

	keyCount, _ := meter.Int64Gauge(
		"litt_chunk_cache_key_count",
		metric.WithDescription("Reports on the number of keys in the cache."),
		metric.WithUnit("{count}"),
	)

	weight, _ := meter.Int64Gauge(
		"litt_chunk_cache_weight_bytes",
		metric.WithDescription("Reports on the weight of the cache in bytes."),
		metric.WithUnit("By"),
	)

	keysAdded, _ := meter.Int64Counter(
		"litt_chunk_cache_keys_added",
		metric.WithDescription("Reports on the number of keys added to the cache."),
		metric.WithUnit("{count}"),
	)

	weightAdded, _ := meter.Int64Counter(
		"litt_chunk_cache_weight_added_bytes",
		metric.WithDescription("Reports on the weight of the entries added to the cache."),
		metric.WithUnit("By"),
	)

	evictionLatency, _ := meter.Float64Histogram(
		"litt_chunk_cache_eviction_latency_seconds",
		metric.WithDescription("Reports on the eviction latency of the cache."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	)

	return &CacheMetrics{
		attrs:           metric.WithAttributes(attribute.String("cache", cacheName)),
		keyCount:        keyCount,
		weight:          weight,
		keysAdded:       keysAdded,
		weightAdded:     weightAdded,
		evictionLatency: evictionLatency,
	}
}

// reportInsertion is used to report an entry being inserted into the cache.
func (m *CacheMetrics) reportInsertion(weight uint64) {
	if m == nil {
		return
	}

	ctx := context.Background()
	m.keysAdded.Add(ctx, 1, m.attrs)
	m.weightAdded.Add(ctx, int64(weight), m.attrs) //nolint:gosec // weight fits int64
}

// reportEviction is used to report an entry being evicted from the cache.
func (m *CacheMetrics) reportEviction(age time.Duration) {
	if m == nil {
		return
	}

	m.evictionLatency.Record(context.Background(), age.Seconds(), m.attrs)
}

// reportCurrentSize is used to report the current size/weight of the cache.
func (m *CacheMetrics) reportCurrentSize(size int, weight uint64) {
	if m == nil {
		return
	}

	ctx := context.Background()
	m.keyCount.Record(ctx, int64(size), m.attrs) //nolint:gosec // size fits int64
	m.weight.Record(ctx, int64(weight), m.attrs) //nolint:gosec // weight fits int64
}
