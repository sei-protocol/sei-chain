package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// Metrics to possibly add in the future:
//  - total disk used, broken down by root
//  - disk available on each root
//  - control loop idle fraction
//    - main control loop
//    - flush loop
//    - shard control loops
//    - keyfile control loop
//  - total number of segments
//  - average segment span (i.e. difference in time between first and last values written to a segment)
//  - segment creation rate
//  - used/unused segment space (useful for detecting shard assignment issues)

const littMeterName = "litt"

// LittDBMetrics encapsulates metrics for a LittDB. Metrics are exported via
// whatever exporter is configured on the global OTel MeterProvider (e.g.
// Prometheus, OTLP). The caller is responsible for setting up the provider
// before calling NewLittDBMetrics (see commonmetrics.SetupOtelPrometheus).
//
// Per-table observations are tagged with a "table" attribute. A nil
// LittDBMetrics acts as a no-op for all Report* methods.
type LittDBMetrics struct {
	// The size of individual tables in the database.
	tableSizeInBytes metric.Int64Gauge

	// The number of keys in individual tables in the database.
	tableKeyCount metric.Int64Gauge

	// The number of currently-open iterators for individual tables in the database.
	openIteratorCount metric.Int64Gauge

	// The number of bytes read from disk since startup.
	bytesReadCounter metric.Int64Counter

	// The number of keys read from disk since startup.
	keysReadCounter metric.Int64Counter

	// The number of cache hits since startup.
	cacheHitCounter metric.Int64Counter

	// The number of cache misses since startup.
	cacheMissCounter metric.Int64Counter

	// Reports on the read latency of the database. This metric includes both cache hits and cache misses.
	readLatency metric.Float64Histogram

	// Reports on the write latency of the database, but only measures the time to read a value when a
	// cache miss occurs.
	cacheMissLatency metric.Float64Histogram

	// The number of bytes written to disk since startup. Only includes values, not metadata.
	bytesWrittenCounter metric.Int64Counter

	// The number of keys written to disk since startup.
	keysWrittenCounter metric.Int64Counter

	// Reports on the write latency of the database.
	writeLatency metric.Float64Histogram

	// The number of times a flush operation has been performed.
	flushCount metric.Int64Counter

	// Reports on the latency of a flush operation.
	flushLatency metric.Float64Histogram

	// Reports on the latency of a flushing segment files. This is a subset of the time spent during a flush operation.
	segmentFlushLatency metric.Float64Histogram

	// Reports on the latency of a keymap flush operation. This is a subset of the time spent during a flush operation.
	keymapFlushLatency metric.Float64Histogram

	// The latency of garbage collection operations.
	garbageCollectionLatency metric.Float64Histogram

	// Metrics for the write cache.
	writeCacheMetrics *util.CacheMetrics

	// Metrics for the read cache.
	readCacheMetrics *util.CacheMetrics
}

// NewLittDBMetrics creates a new LittDBMetrics instance backed by the global
// OTel MeterProvider. The caller must configure a MeterProvider with a
// Prometheus or other exporter before calling this (e.g. via
// commonmetrics.SetupOtelPrometheus).
func NewLittDBMetrics() *LittDBMetrics {
	meter := otel.Meter(littMeterName)

	tableSizeInBytes, _ := meter.Int64Gauge(
		"litt_table_size_bytes",
		metric.WithDescription("The size of individual tables in the database in bytes."),
		metric.WithUnit("By"),
	)

	tableKeyCount, _ := meter.Int64Gauge(
		"litt_table_key_count",
		metric.WithDescription("The number of keys in individual tables in the database."),
		metric.WithUnit("{count}"),
	)

	openIteratorCount, _ := meter.Int64Gauge(
		"litt_open_iterator_count",
		metric.WithDescription(
			"The number of currently-open iterators for individual tables in the database. "+
				"A persistently nonzero value indicates a leaked iterator, which suspends garbage collection."),
		metric.WithUnit("{count}"),
	)

	bytesReadCounter, _ := meter.Int64Counter(
		"litt_bytes_read",
		metric.WithDescription("The number of bytes read from disk since startup."),
		metric.WithUnit("By"),
	)

	keysReadCounter, _ := meter.Int64Counter(
		"litt_keys_read",
		metric.WithDescription("The number of keys read from disk since startup."),
		metric.WithUnit("{count}"),
	)

	cacheHitCounter, _ := meter.Int64Counter(
		"litt_cache_hits",
		metric.WithDescription("The number of cache hits since startup."),
		metric.WithUnit("{count}"),
	)

	cacheMissCounter, _ := meter.Int64Counter(
		"litt_cache_misses",
		metric.WithDescription("The number of cache misses since startup."),
		metric.WithUnit("{count}"),
	)

	readLatency, _ := meter.Float64Histogram(
		"litt_read_latency_seconds",
		metric.WithDescription(
			"Reports on the read latency of the database. "+
				"This metric includes both cache hits and cache misses."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	)

	cacheMissLatency, _ := meter.Float64Histogram(
		"litt_cache_miss_latency_seconds",
		metric.WithDescription(
			"Reports on the read latency of the database, "+
				"but only measures the time to read a value when a cache miss occurs."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	)

	bytesWrittenCounter, _ := meter.Int64Counter(
		"litt_bytes_written",
		metric.WithDescription("The number of bytes written to disk since startup. Only includes values, not metadata."),
		metric.WithUnit("By"),
	)

	keysWrittenCounter, _ := meter.Int64Counter(
		"litt_keys_written",
		metric.WithDescription("The number of keys written to disk since startup."),
		metric.WithUnit("{count}"),
	)

	writeLatency, _ := meter.Float64Histogram(
		"litt_write_latency_seconds",
		metric.WithDescription("Reports on the write latency of the database."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	)

	flushCount, _ := meter.Int64Counter(
		"litt_flush_count",
		metric.WithDescription("The number of times a flush operation has been performed."),
		metric.WithUnit("{count}"),
	)

	flushLatency, _ := meter.Float64Histogram(
		"litt_flush_latency_seconds",
		metric.WithDescription("Reports on the latency of a flush operation."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	)

	segmentFlushLatency, _ := meter.Float64Histogram(
		"litt_segment_flush_latency_seconds",
		metric.WithDescription("Reports on segment flush latency. This is a subset of the time spent during a flush operation."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	)

	keymapFlushLatency, _ := meter.Float64Histogram(
		"litt_keymap_flush_latency_seconds",
		metric.WithDescription(
			"Reports on the latency of a keymap flush operation. "+
				"This is a subset of the time spent during a flush operation."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	)

	garbageCollectionLatency, _ := meter.Float64Histogram(
		"litt_garbage_collection_latency_seconds",
		metric.WithDescription("Reports on the latency of garbage collection operations."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	)

	writeCacheMetrics := util.NewCacheMetrics("chunk_write")
	readCacheMetrics := util.NewCacheMetrics("chunk_read")

	return &LittDBMetrics{
		tableSizeInBytes:         tableSizeInBytes,
		tableKeyCount:            tableKeyCount,
		openIteratorCount:        openIteratorCount,
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
		garbageCollectionLatency: garbageCollectionLatency,
		segmentFlushLatency:      segmentFlushLatency,
		keymapFlushLatency:       keymapFlushLatency,
		writeCacheMetrics:        writeCacheMetrics,
		readCacheMetrics:         readCacheMetrics,
	}
}

// tableAttr returns the OTel measurement option that tags an observation with
// the given table name. Allocated per call rather than cached because callers
// pass arbitrary table names; for hot-path call sites consider caching upstream.
func tableAttr(tableName string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("table", tableName))
}

// CollectPeriodicMetrics is a method that is periodically called to collect metrics. Tables are not permitted to be
// added or dropped while this method is running.
func (m *LittDBMetrics) CollectPeriodicMetrics(tables map[string]litt.ManagedTable) {
	if m == nil {
		return
	}

	ctx := context.Background()
	for _, table := range tables {
		tableName := table.Name()
		attrs := tableAttr(tableName)

		tableSize := table.Size()
		m.tableSizeInBytes.Record(ctx, int64(tableSize), attrs) //nolint:gosec // table size fits int64

		tableKeyCount := table.KeyCount()
		m.tableKeyCount.Record(ctx, int64(tableKeyCount), attrs) //nolint:gosec // key count fits int64
	}
}

// ReportOpenIteratorCount reports the current number of open iterators for a table. A persistently
// nonzero value when no iteration is expected indicates a leaked iterator, which suspends garbage
// collection for the table.
func (m *LittDBMetrics) ReportOpenIteratorCount(tableName string, count int64) {
	if m == nil {
		return
	}
	m.openIteratorCount.Record(context.Background(), count, tableAttr(tableName))
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
	attrs := tableAttr(tableName)

	m.bytesReadCounter.Add(ctx, int64(dataSize), attrs) //nolint:gosec // data size fits int64
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
	attrs := tableAttr(tableName)

	m.bytesWrittenCounter.Add(ctx, int64(dataSize), attrs) //nolint:gosec // data size fits int64
	m.keysWrittenCounter.Add(ctx, int64(batchSize), attrs) //nolint:gosec // batch size fits int64
	m.writeLatency.Record(ctx, latency.Seconds(), attrs)
}

// ReportFlushOperation reports the results of a flush operation.
func (m *LittDBMetrics) ReportFlushOperation(tableName string, latency time.Duration) {
	if m == nil {
		return
	}

	ctx := context.Background()
	attrs := tableAttr(tableName)

	m.flushCount.Add(ctx, 1, attrs)
	m.flushLatency.Record(ctx, latency.Seconds(), attrs)
}

// ReportSegmentFlushLatency reports the amount of time taken to flush value files.
func (m *LittDBMetrics) ReportSegmentFlushLatency(tableName string, latency time.Duration) {
	if m == nil {
		return
	}

	m.segmentFlushLatency.Record(context.Background(), latency.Seconds(), tableAttr(tableName))
}

// ReportKeymapFlushLatency reports the amount of time taken to flush the keymap.
func (m *LittDBMetrics) ReportKeymapFlushLatency(tableName string, latency time.Duration) {
	if m == nil {
		return
	}

	m.keymapFlushLatency.Record(context.Background(), latency.Seconds(), tableAttr(tableName))
}

// ReportGarbageCollectionLatency reports the latency of a garbage collection operation.
func (m *LittDBMetrics) ReportGarbageCollectionLatency(tableName string, latency time.Duration) {
	if m == nil {
		return
	}

	m.garbageCollectionLatency.Record(context.Background(), latency.Seconds(), tableAttr(tableName))
}

func (m *LittDBMetrics) GetWriteCacheMetrics() *util.CacheMetrics {
	if m == nil {
		return nil
	}
	return m.writeCacheMetrics
}

func (m *LittDBMetrics) GetReadCacheMetrics() *util.CacheMetrics {
	if m == nil {
		return nil
	}
	return m.readCacheMetrics
}
