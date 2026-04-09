package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	sharedmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	cache "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util/datacache"
)

const littMeterName = "litt"

// TableInfo is implemented by any type that can report its name, on-disk size,
// and key count. litt.ManagedTable satisfies this interface.
type TableInfo interface {
	Name() string
	Size() uint64
	KeyCount() uint64
}

// LittDBMetrics encapsulates OTel metrics for a LittDB instance.
// A nil receiver is safe: all report methods are no-ops.
type LittDBMetrics struct {
	tableSizeInBytes metric.Int64Gauge
	tableKeyCount    metric.Int64Gauge

	bytesReadCounter metric.Int64Counter
	keysReadCounter  metric.Int64Counter
	cacheHitCounter  metric.Int64Counter
	cacheMissCounter metric.Int64Counter
	readLatency      metric.Float64Histogram
	cacheMissLatency metric.Float64Histogram

	bytesWrittenCounter metric.Int64Counter
	keysWrittenCounter  metric.Int64Counter
	writeLatency        metric.Float64Histogram

	flushCount               metric.Int64Counter
	flushLatency             metric.Float64Histogram
	segmentFlushLatency      metric.Float64Histogram
	keymapFlushLatency       metric.Float64Histogram
	garbageCollectionLatency metric.Float64Histogram

	writeCacheMetrics *cache.CacheMetrics
	readCacheMetrics  *cache.CacheMetrics

	keymapBatchCount metric.Int64Counter
	keymapBatchSize  metric.Float64Histogram

	segmentSealCount     metric.Int64Counter
	segmentSealSizeBytes metric.Float64Histogram
	segmentSealKeyCount  metric.Float64Histogram

	controlLoopPhaseTimer   *sharedmetrics.PhaseTimer
	keymapManagerPhaseTimer *sharedmetrics.PhaseTimer
	flushLoopPhaseTimer     *sharedmetrics.PhaseTimer

	*channelObserver
}

// NewLittDBMetrics creates a LittDBMetrics using the global OTel MeterProvider.
func NewLittDBMetrics() *LittDBMetrics {
	meter := otel.Meter(littMeterName)
	latencyOpts := metric.WithExplicitBucketBoundaries(sharedmetrics.LatencyBuckets...)

	tableSizeInBytes, _ := meter.Int64Gauge(
		"litt_table_size",
		metric.WithDescription("On-disk size of individual tables"),
		metric.WithUnit("By"),
	)
	tableKeyCount, _ := meter.Int64Gauge(
		"litt_table_key_count",
		metric.WithDescription("Number of keys in individual tables"),
		metric.WithUnit("{count}"),
	)

	bytesReadCounter, _ := meter.Int64Counter(
		"litt_bytes_read",
		metric.WithDescription("Bytes read from disk since startup"),
		metric.WithUnit("By"),
	)
	keysReadCounter, _ := meter.Int64Counter(
		"litt_keys_read",
		metric.WithDescription("Keys read from disk since startup"),
		metric.WithUnit("{count}"),
	)
	cacheHitCounter, _ := meter.Int64Counter(
		"litt_cache_hits",
		metric.WithDescription("Read-path cache hits since startup"),
		metric.WithUnit("{count}"),
	)
	cacheMissCounter, _ := meter.Int64Counter(
		"litt_cache_misses",
		metric.WithDescription("Read-path cache misses since startup"),
		metric.WithUnit("{count}"),
	)
	readLatency, _ := meter.Float64Histogram(
		"litt_read_latency",
		metric.WithDescription("Read latency (includes cache hits and misses)"),
		metric.WithUnit("s"),
		latencyOpts,
	)
	cacheMissLatency, _ := meter.Float64Histogram(
		"litt_cache_miss_latency",
		metric.WithDescription("Read latency on cache miss only"),
		metric.WithUnit("s"),
		latencyOpts,
	)

	bytesWrittenCounter, _ := meter.Int64Counter(
		"litt_bytes_written",
		metric.WithDescription("Bytes written to disk since startup (values only)"),
		metric.WithUnit("By"),
	)
	keysWrittenCounter, _ := meter.Int64Counter(
		"litt_keys_written",
		metric.WithDescription("Keys written to disk since startup"),
		metric.WithUnit("{count}"),
	)
	writeLatency, _ := meter.Float64Histogram(
		"litt_write_latency",
		metric.WithDescription("Write latency"),
		metric.WithUnit("s"),
		latencyOpts,
	)

	flushCount, _ := meter.Int64Counter(
		"litt_flush_count",
		metric.WithDescription("Flush operations completed"),
		metric.WithUnit("{count}"),
	)
	flushLatency, _ := meter.Float64Histogram(
		"litt_flush_latency",
		metric.WithDescription("End-to-end flush latency"),
		metric.WithUnit("s"),
		latencyOpts,
	)
	segmentFlushLatency, _ := meter.Float64Histogram(
		"litt_segment_flush_latency",
		metric.WithDescription("Segment file flush latency (subset of flush)"),
		metric.WithUnit("s"),
		latencyOpts,
	)
	keymapFlushLatency, _ := meter.Float64Histogram(
		"litt_keymap_flush_latency",
		metric.WithDescription("Keymap flush latency (subset of flush)"),
		metric.WithUnit("s"),
		latencyOpts,
	)
	garbageCollectionLatency, _ := meter.Float64Histogram(
		"litt_gc_latency",
		metric.WithDescription("Garbage collection latency"),
		metric.WithUnit("s"),
		latencyOpts,
	)

	keymapBatchCount, _ := meter.Int64Counter(
		"litt_keymap_batch_count",
		metric.WithDescription("Number of batches committed to the keymap"),
		metric.WithUnit("{count}"),
	)
	keymapBatchSize, _ := meter.Float64Histogram(
		"litt_keymap_batch_size",
		metric.WithDescription("Number of keys per keymap batch"),
		metric.WithUnit("{keys}"),
		metric.WithExplicitBucketBoundaries(
			1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 50000, 100000),
	)

	segmentSealCount, _ := meter.Int64Counter(
		"litt_segment_seal_count",
		metric.WithDescription("Number of segments sealed"),
		metric.WithUnit("{count}"),
	)
	segmentSealSizeBytes, _ := meter.Float64Histogram(
		"litt_segment_seal_size",
		metric.WithDescription("Size of segments at seal time"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(
			1<<20, 4<<20, 16<<20, 64<<20, 256<<20, 1<<30, 4<<30),
	)
	segmentSealKeyCount, _ := meter.Float64Histogram(
		"litt_segment_seal_keys",
		metric.WithDescription("Number of keys in segments at seal time"),
		metric.WithUnit("{keys}"),
		metric.WithExplicitBucketBoundaries(
			100, 500, 1000, 5000, 10000, 50000, 100000, 500000, 1000000),
	)

	writeCacheMetrics := cache.NewCacheMetrics(meter, "chunk_write")
	readCacheMetrics := cache.NewCacheMetrics(meter, "chunk_read")

	controlLoopPhaseTimer := sharedmetrics.NewPhaseTimer(meter, "litt_control_loop")
	keymapManagerPhaseTimer := sharedmetrics.NewPhaseTimer(meter, "litt_keymap_manager")
	flushLoopPhaseTimer := sharedmetrics.NewPhaseTimer(meter, "litt_flush_loop")

	return &LittDBMetrics{
		tableSizeInBytes:         tableSizeInBytes,
		tableKeyCount:            tableKeyCount,
		bytesReadCounter:         bytesReadCounter,
		keysReadCounter:          keysReadCounter,
		cacheHitCounter:          cacheHitCounter,
		cacheMissCounter:         cacheMissCounter,
		readLatency:              readLatency,
		cacheMissLatency:         cacheMissLatency,
		bytesWrittenCounter:      bytesWrittenCounter,
		keysWrittenCounter:       keysWrittenCounter,
		writeLatency:             writeLatency,
		flushCount:               flushCount,
		flushLatency:             flushLatency,
		segmentFlushLatency:      segmentFlushLatency,
		keymapFlushLatency:       keymapFlushLatency,
		garbageCollectionLatency: garbageCollectionLatency,
		keymapBatchCount:         keymapBatchCount,
		keymapBatchSize:          keymapBatchSize,
		segmentSealCount:         segmentSealCount,
		segmentSealSizeBytes:     segmentSealSizeBytes,
		segmentSealKeyCount:      segmentSealKeyCount,
		writeCacheMetrics:        writeCacheMetrics,
		readCacheMetrics:         readCacheMetrics,
		controlLoopPhaseTimer:    controlLoopPhaseTimer,
		keymapManagerPhaseTimer:  keymapManagerPhaseTimer,
		flushLoopPhaseTimer:      flushLoopPhaseTimer,
		channelObserver:          newChannelObserver(meter),
	}
}

// RegisterChannel registers (or replaces) a channel size function to be
// observed on each periodic metrics collection cycle.
func (m *LittDBMetrics) RegisterChannel(name string, sizeFunc func() int) {
	if m == nil {
		return
	}
	m.register(name, sizeFunc)
}

// CollectPeriodicMetrics snapshots table sizes, key counts, and channel
// depths into gauges.
func (m *LittDBMetrics) CollectPeriodicMetrics(tables []TableInfo) {
	if m == nil {
		return
	}
	ctx := context.Background()
	for _, table := range tables {
		attrs := metric.WithAttributes(attribute.String("table", table.Name()))
		m.tableSizeInBytes.Record(ctx, int64(table.Size()), attrs)  //nolint:gosec
		m.tableKeyCount.Record(ctx, int64(table.KeyCount()), attrs) //nolint:gosec
	}
	m.collectOnce()
}

// ReportReadOperation reports the results of a read operation.
func (m *LittDBMetrics) ReportReadOperation(
	tableName string,
	latency time.Duration,
	dataSize uint64,
	cacheHit bool) {

	if m == nil {
		return
	}
	ctx := context.Background()
	attrs := metric.WithAttributes(attribute.String("table", tableName))

	m.bytesReadCounter.Add(ctx, int64(dataSize), attrs) //nolint:gosec
	m.keysReadCounter.Add(ctx, 1, attrs)
	m.readLatency.Record(ctx, latency.Seconds(), attrs)

	if cacheHit {
		m.cacheHitCounter.Add(ctx, 1, attrs)
	} else {
		m.cacheMissCounter.Add(ctx, 1, attrs)
		m.cacheMissLatency.Record(ctx, latency.Seconds(), attrs)
	}
}

// ReportWriteOperation reports the results of a write operation.
func (m *LittDBMetrics) ReportWriteOperation(
	tableName string,
	latency time.Duration,
	batchSize uint64,
	dataSize uint64) {

	if m == nil {
		return
	}
	ctx := context.Background()
	attrs := metric.WithAttributes(attribute.String("table", tableName))

	m.bytesWrittenCounter.Add(ctx, int64(dataSize), attrs) //nolint:gosec
	m.keysWrittenCounter.Add(ctx, int64(batchSize), attrs) //nolint:gosec
	m.writeLatency.Record(ctx, latency.Seconds(), attrs)
}

// ReportFlushOperation reports the results of a flush operation.
func (m *LittDBMetrics) ReportFlushOperation(tableName string, latency time.Duration) {
	if m == nil {
		return
	}
	ctx := context.Background()
	attrs := metric.WithAttributes(attribute.String("table", tableName))
	m.flushCount.Add(ctx, 1, attrs)
	m.flushLatency.Record(ctx, latency.Seconds(), attrs)
}

// ReportSegmentFlushLatency reports the time taken to flush value files.
func (m *LittDBMetrics) ReportSegmentFlushLatency(tableName string, latency time.Duration) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("table", tableName))
	m.segmentFlushLatency.Record(context.Background(), latency.Seconds(), attrs)
}

// ReportKeymapFlushLatency reports the time taken to flush the keymap.
func (m *LittDBMetrics) ReportKeymapFlushLatency(tableName string, latency time.Duration) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("table", tableName))
	m.keymapFlushLatency.Record(context.Background(), latency.Seconds(), attrs)
}

// ReportGarbageCollectionLatency reports the latency of a garbage collection operation.
func (m *LittDBMetrics) ReportGarbageCollectionLatency(tableName string, latency time.Duration) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("table", tableName))
	m.garbageCollectionLatency.Record(context.Background(), latency.Seconds(), attrs)
}

// ReportKeymapBatch reports that a batch of the given size was committed to the keymap.
func (m *LittDBMetrics) ReportKeymapBatch(tableName string, batchSize int) {
	if m == nil {
		return
	}
	ctx := context.Background()
	attrs := metric.WithAttributes(attribute.String("table", tableName))
	m.keymapBatchCount.Add(ctx, 1, attrs)
	m.keymapBatchSize.Record(ctx, float64(batchSize), attrs)
}

// ReportSegmentSealed reports that a segment was sealed with the given size, key count, and reason.
func (m *LittDBMetrics) ReportSegmentSealed(tableName string, sizeBytes uint64, keyCount uint32, reason string) {
	if m == nil {
		return
	}
	ctx := context.Background()
	tableAttr := metric.WithAttributes(attribute.String("table", tableName))
	m.segmentSealSizeBytes.Record(ctx, float64(sizeBytes), tableAttr)
	m.segmentSealKeyCount.Record(ctx, float64(keyCount), tableAttr)
	m.segmentSealCount.Add(ctx, 1, metric.WithAttributes(
		attribute.String("table", tableName),
		attribute.String("reason", reason),
	))
}

func (m *LittDBMetrics) GetWriteCacheMetrics() *cache.CacheMetrics {
	if m == nil {
		return nil
	}
	return m.writeCacheMetrics
}

func (m *LittDBMetrics) GetReadCacheMetrics() *cache.CacheMetrics {
	if m == nil {
		return nil
	}
	return m.readCacheMetrics
}

// SetKeymapManagerPhase transitions the keymap manager phase timer to the given phase.
// Passing an empty string resets the phase timer.
func (m *LittDBMetrics) SetKeymapManagerPhase(phase string) {
	if m == nil {
		return
	}
	if phase == "" {
		m.keymapManagerPhaseTimer.Reset()
	} else {
		m.keymapManagerPhaseTimer.SetPhase(phase)
	}
}

// SetControlLoopPhase transitions the control loop phase timer to the given phase.
// Passing an empty string resets the phase timer.
func (m *LittDBMetrics) SetControlLoopPhase(phase string) {
	if m == nil {
		return
	}
	if phase == "" {
		m.controlLoopPhaseTimer.Reset()
	} else {
		m.controlLoopPhaseTimer.SetPhase(phase)
	}
}

// SetFlushLoopPhase transitions the flush loop phase timer to the given phase.
// Passing an empty string resets the phase timer.
func (m *LittDBMetrics) SetFlushLoopPhase(phase string) {
	if m == nil {
		return
	}
	if phase == "" {
		m.flushLoopPhaseTimer.Reset()
	} else {
		m.flushLoopPhaseTimer.SetPhase(phase)
	}
}
