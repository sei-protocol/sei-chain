package view

import (
	"context"
	"sync/atomic"
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

	// One per shard. Reads count hits and misses here, and collectLoop publishes the totals to hits
	// and misses on each scrape.
	shards []*shardMetrics

	// Closed by collectLoop when it exits. awaitStopped blocks on it so manager Close can
	// guarantee the scrape goroutine is gone before returning.
	collectDone chan struct{}
}

// shardMetrics is one shard's handle on its view manager's metrics. It counts hits and misses in
// atomics of its own rather than recording each read into an OTel instrument, which every reader
// thread would otherwise contend on. All report methods are nil-safe.
type shardMetrics struct {
	manager *ViewManagerMetrics
	hits    atomic.Int64
	misses  atomic.Int64

	// Pads the struct to a cache line, so that shards counting concurrently do not share one.
	_ [cacheLineBytes - 24]byte
}

// cacheLineBytes is the cache line size shardMetrics is padded to.
const cacheLineBytes = 64

// newViewManagerMetrics creates a ViewManagerMetrics that records cache statistics via OTel, with one
// shardMetrics per shard. A background goroutine scrapes cache size and publishes the shards' hit and
// miss counts every scrapeInterval until ctx is cancelled. The cacheName is attached as the "cache"
// attribute to all recorded metrics, enabling multiple cache instances to be distinguished in
// dashboards.
//
// Multiple instances are safe: OTel instrument registration is idempotent, so each
// call receives references to the same underlying instruments. The "cache" attribute
// distinguishes series (e.g. view_manager_hits{cache="state"}).
func newViewManagerMetrics(
	ctx context.Context,
	cacheName string,
	scrapeInterval time.Duration,
	getSize func() (bytes uint64, entries uint64),
	shardCount uint64,
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
		shards:         make([]*shardMetrics, shardCount),
		collectDone:    make(chan struct{}),
	}
	for i := range cm.shards {
		cm.shards[i] = &shardMetrics{manager: cm}
	}

	go cm.collectLoop(ctx, scrapeInterval, getSize)

	return cm
}

// shard returns the metrics handle for the shard at index.
func (cm *ViewManagerMetrics) shard(index int) *shardMetrics {
	return cm.shards[index]
}

// publishCounts adds the hits and misses the shards have counted since the last call to the OTel
// counters.
func (cm *ViewManagerMetrics) publishCounts(ctx context.Context) {
	var hits, misses int64
	for _, s := range cm.shards {
		hits += s.hits.Swap(0)
		misses += s.misses.Swap(0)
	}
	if hits > 0 {
		cm.hits.Add(ctx, hits, cm.attrs)
	}
	if misses > 0 {
		cm.misses.Add(ctx, misses, cm.attrs)
	}
}

func (m *shardMetrics) reportCacheHits(count int64) {
	if m == nil {
		return
	}
	m.hits.Add(count)
}

func (m *shardMetrics) reportCacheMisses(count int64) {
	if m == nil {
		return
	}
	m.misses.Add(count)
}

// reportCacheMissLatency records how long a miss took to resolve. It is recorded directly, since a
// miss already waits on a DB read that costs far more than the recording.
func (m *shardMetrics) reportCacheMissLatency(latency time.Duration) {
	if m == nil {
		return
	}
	m.manager.missLatency.Record(context.Background(), latency.Seconds(), m.manager.attrs)
}

// collectLoop periodically scrapes cache size from the provided function, records it as gauge
// values, and publishes the shards' hit and miss counts. It exits when ctx is cancelled, publishing
// the counts accumulated since the last scrape first.
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
			cm.publishCounts(context.WithoutCancel(ctx))
			return
		case <-ticker.C:
			bytes, entries := getSize()
			// G115: safe — cache size and entry count fit in int64.
			cm.sizeBytes.Record(ctx, int64(bytes), cm.attrs)     //nolint:gosec
			cm.sizeEntries.Record(ctx, int64(entries), cm.attrs) //nolint:gosec
			cm.publishCounts(ctx)
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
