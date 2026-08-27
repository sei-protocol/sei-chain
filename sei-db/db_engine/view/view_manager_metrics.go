package view

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

const viewManagerMeterName = "seidb_view_manager"

// ViewManagerMetrics records OTel metrics for a view manager instance.
// All report methods are nil-safe: if the receiver is nil, they are no-ops,
// allowing the manager to call them unconditionally regardless of whether metrics
// are enabled.
//
// The cacheName is used as the "cache" attribute on all recorded metrics,
// enabling multiple cache instances to be distinguished in dashboards.
type ViewManagerMetrics struct {
	// Pre-computed attribute option reused on every recording to avoid
	// per-call allocations on the hot path.
	attrs metric.MeasurementOption

	sizeBytes      metric.Int64Gauge
	sizeEntries    metric.Int64Gauge
	hits           metric.Int64Counter
	misses         metric.Int64Counter
	missLatency    metric.Float64Histogram
	viewPhaseTimer *metrics.PhaseTimer

	// Closed by collectLoop when it exits. awaitStopped blocks on it so manager Close can
	// guarantee the scrape goroutine is gone before returning.
	collectDone chan struct{}
}

// newViewManagerMetrics creates a ViewManagerMetrics that records cache statistics via OTel.
// A background goroutine scrapes cache size every scrapeInterval until ctx is
// cancelled. The cacheName is attached as the "cache" attribute to all recorded
// metrics, enabling multiple cache instances to be distinguished in dashboards.
//
// Multiple instances are safe: OTel instrument registration is idempotent, so each
// call receives references to the same underlying instruments. The "cache" attribute
// distinguishes series (e.g. view_manager_hits{cache="state"}).
func newViewManagerMetrics(
	ctx context.Context,
	cacheName string,
	scrapeInterval time.Duration,
	getSize func() (bytes uint64, entries uint64),
) *ViewManagerMetrics {
	meter := otel.Meter(viewManagerMeterName)

	sizeBytes, _ := meter.Int64Gauge(
		"view_manager_size_bytes",
		metric.WithDescription("Current cache size in bytes"),
		metric.WithUnit("By"),
	)
	sizeEntries, _ := meter.Int64Gauge(
		"view_manager_size_entries",
		metric.WithDescription("Current number of entries in the cache"),
		metric.WithUnit("{count}"),
	)
	hits, _ := meter.Int64Counter(
		"view_manager_hits",
		metric.WithDescription("Total number of cache hits"),
		metric.WithUnit("{count}"),
	)
	misses, _ := meter.Int64Counter(
		"view_manager_misses",
		metric.WithDescription("Total number of cache misses"),
		metric.WithUnit("{count}"),
	)
	missLatency, _ := meter.Float64Histogram(
		"view_manager_miss_latency",
		metric.WithDescription("Time taken to resolve a cache miss from the backing store"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(metrics.LatencyBuckets...),
	)
	cacheAttr := attribute.String("cache", cacheName)
	viewPhaseTimer := metrics.NewPhaseTimer(meter, "view_manager_view", cacheAttr)

	cm := &ViewManagerMetrics{
		attrs:          metric.WithAttributes(cacheAttr),
		sizeBytes:      sizeBytes,
		sizeEntries:    sizeEntries,
		hits:           hits,
		misses:         misses,
		missLatency:    missLatency,
		viewPhaseTimer: viewPhaseTimer,
		collectDone:    make(chan struct{}),
	}

	go cm.collectLoop(ctx, scrapeInterval, getSize)

	return cm
}

func (cm *ViewManagerMetrics) reportCacheHits(count int64) {
	if cm == nil {
		return
	}
	cm.hits.Add(context.Background(), count, cm.attrs)
}

func (cm *ViewManagerMetrics) reportCacheMisses(count int64) {
	if cm == nil {
		return
	}
	cm.misses.Add(context.Background(), count, cm.attrs)
}

func (cm *ViewManagerMetrics) reportCacheMissLatency(latency time.Duration) {
	if cm == nil {
		return
	}
	cm.missLatency.Record(context.Background(), latency.Seconds(), cm.attrs)
}

// collectLoop periodically scrapes cache size from the provided function
// and records it as gauge values. It exits when ctx is cancelled.
func (cm *ViewManagerMetrics) collectLoop(
	ctx context.Context,
	interval time.Duration,
	getSize func() (bytes uint64, entries uint64),
) {

	if cm == nil {
		return
	}
	defer close(cm.collectDone)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			bytes, entries := getSize()
			// G115: safe — cache size and entry count fit in int64.
			cm.sizeBytes.Record(ctx, int64(bytes), cm.attrs)     //nolint:gosec
			cm.sizeEntries.Record(ctx, int64(entries), cm.attrs) //nolint:gosec
		}
	}
}

// awaitStopped blocks until the collect loop has exited. Nil-safe; returns immediately when
// metrics are disabled.
func (cm *ViewManagerMetrics) awaitStopped() {
	if cm == nil {
		return
	}
	<-cm.collectDone
}

// setViewPhase sets the phase for the view phase timer.
func (cm *ViewManagerMetrics) setViewPhase(phase string) {
	if cm == nil {
		return
	}
	if phase == "" {
		cm.viewPhaseTimer.Reset()

	} else {
		cm.viewPhaseTimer.SetPhase(phase)
	}
}
