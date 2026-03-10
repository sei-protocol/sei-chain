package pebblecache

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const cacheMeterName = "seidb_pebblecache"

// CacheMetrics records OTel metrics for a pebblecache instance.
// All report methods are nil-safe: if the receiver is nil, they are no-ops,
// allowing the cache to call them unconditionally regardless of whether metrics
// are enabled.
//
// The cacheName is used as the "cache" attribute on all recorded metrics,
// enabling multiple cache instances to be distinguished in dashboards.
type CacheMetrics struct {
	// Pre-computed attribute option reused on every recording to avoid
	// per-call allocations on the hot path.
	attrs metric.MeasurementOption

	sizeBytes   metric.Int64Gauge
	sizeEntries metric.Int64Gauge
	hits        metric.Int64Counter
	misses      metric.Int64Counter
	missLatency metric.Float64Histogram
}

// newCacheMetrics creates a CacheMetrics that records cache statistics via OTel.
// A background goroutine scrapes cache size every scrapeInterval until ctx is
// cancelled. The cacheName is attached as the "cache" attribute to all recorded
// metrics, enabling multiple cache instances to be distinguished in dashboards.
//
// Multiple instances are safe: OTel instrument registration is idempotent, so each
// call receives references to the same underlying instruments. The "cache" attribute
// distinguishes series (e.g. pebblecache_hits{cache="state"}).
func newCacheMetrics(
	ctx context.Context,
	cacheName string,
	scrapeInterval time.Duration,
	getSize func() (bytes int64, entries int64),
) *CacheMetrics {
	meter := otel.Meter(cacheMeterName)

	sizeBytes, _ := meter.Int64Gauge(
		"pebblecache_size_bytes",
		metric.WithDescription("Current cache size in bytes"),
		metric.WithUnit("By"),
	)
	sizeEntries, _ := meter.Int64Gauge(
		"pebblecache_size_entries",
		metric.WithDescription("Current number of entries in the cache"),
		metric.WithUnit("{count}"),
	)
	hits, _ := meter.Int64Counter(
		"pebblecache_hits",
		metric.WithDescription("Total number of cache hits"),
		metric.WithUnit("{count}"),
	)
	misses, _ := meter.Int64Counter(
		"pebblecache_misses",
		metric.WithDescription("Total number of cache misses"),
		metric.WithUnit("{count}"),
	)
	missLatency, _ := meter.Float64Histogram(
		"pebblecache_miss_latency",
		metric.WithDescription("Time taken to resolve a cache miss from the backing store"),
		metric.WithUnit("s"),
	)

	cm := &CacheMetrics{
		attrs:       metric.WithAttributes(attribute.String("cache", cacheName)),
		sizeBytes:   sizeBytes,
		sizeEntries: sizeEntries,
		hits:        hits,
		misses:      misses,
		missLatency: missLatency,
	}

	go cm.collectLoop(ctx, scrapeInterval, getSize)

	return cm
}

func (cm *CacheMetrics) reportCacheHits(count int64) {
	if cm == nil {
		return
	}
	cm.hits.Add(context.Background(), count, cm.attrs)
}

func (cm *CacheMetrics) reportCacheMisses(count int64) {
	if cm == nil {
		return
	}
	cm.misses.Add(context.Background(), count, cm.attrs)
}

func (cm *CacheMetrics) reportCacheMissLatency(latency time.Duration) {
	if cm == nil {
		return
	}
	cm.missLatency.Record(context.Background(), latency.Seconds(), cm.attrs)
}

// collectLoop periodically scrapes cache size from the provided function
// and records it as gauge values. It exits when ctx is cancelled.
func (cm *CacheMetrics) collectLoop(
	ctx context.Context,
	interval time.Duration,
	getSize func() (bytes int64, entries int64),
) {

	if cm == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			bytes, entries := getSize()
			cm.sizeBytes.Record(ctx, bytes, cm.attrs)
			cm.sizeEntries.Record(ctx, entries, cm.attrs)
		}
	}
}
