package metrics

import (
	"time"

	"github.com/Layr-Labs/eigenda/common"
	"github.com/Layr-Labs/eigenda/common/cache"
	"github.com/Layr-Labs/eigenda/litt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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

// LittDBMetrics encapsulates metrics for a LittDB.
type LittDBMetrics struct {
	// The size of individual tables in the database.
	tableSizeInBytes *prometheus.GaugeVec

	// The number of keys in individual tables in the database.
	tableKeyCount *prometheus.GaugeVec

	// The number of bytes read from disk since startup.
	bytesReadCounter *prometheus.CounterVec

	// The number of keys read from disk since startup.
	keysReadCounter *prometheus.CounterVec

	// The number of cache hits since startup.
	cacheHitCounter *prometheus.CounterVec

	// The number of cache misses since startup.
	cacheMissCounter *prometheus.CounterVec

	// Reports on the read latency of the database. This metric includes both cache hits and cache misses.
	readLatency *prometheus.SummaryVec

	// Reports on the write latency of the database, but only measures the time to read a value when a
	// cache miss occurs.
	cacheMissLatency *prometheus.SummaryVec

	// The number of bytes written to disk since startup. Only includes values, not metadata.
	bytesWrittenCounter *prometheus.CounterVec

	// The number of keys written to disk since startup.
	keysWrittenCounter *prometheus.CounterVec

	// Reports on the write latency of the database.
	writeLatency *prometheus.SummaryVec

	// The number of times a flush operation has been performed.
	flushCount *prometheus.CounterVec

	// Reports on the latency of a flush operation.
	flushLatency *prometheus.SummaryVec

	// Reports on the latency of a flushing segment files. This is a subset of the time spent during a flush operation.
	segmentFlushLatency *prometheus.SummaryVec

	// Reports on the latency of a keymap flush operation. This is a subset of the time spent during a flush operation.
	keymapFlushLatency *prometheus.SummaryVec

	// The latency of garbage collection operations.1
	garbageCollectionLatency *prometheus.SummaryVec

	// Metrics for the write cache.
	writeCacheMetrics *cache.CacheMetrics

	// Metrics for the read cache.
	readCacheMetrics *cache.CacheMetrics
}

// NewLittDBMetrics creates a new LittDBMetrics instance.
func NewLittDBMetrics(registry *prometheus.Registry, namespace string) *LittDBMetrics {
	if registry == nil {
		return nil
	}

	objectives := map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001}

	tableSizeInBytes := promauto.With(registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "table_size_bytes",
			Help:      "The size of individual tables in the database in bytes.",
		},
		[]string{"table"},
	)

	tableKeyCount := promauto.With(registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "table_key_count",
			Help:      "The number of keys in individual tables in the database.",
		},
		[]string{"table"},
	)

	bytesReadCounter := promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "bytes_read",
			Help:      "The number of bytes read from disk since startup.",
		},
		[]string{"table"},
	)

	keysReadCounter := promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "keys_read",
			Help:      "The number of keys read from disk since startup.",
		},
		[]string{"table"},
	)

	cacheHitCounter := promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_hits",
			Help:      "The number of cache hits since startup.",
		},
		[]string{"table"},
	)

	cacheMissCounter := promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_misses",
			Help:      "The number of cache misses since startup.",
		},
		[]string{"table"},
	)

	readLatency := promauto.With(registry).NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: namespace,
			Name:      "read_latency_ms",
			Help: "Reports on the read latency of the database. " +
				"This metric includes both cache hits and cache misses.",
			Objectives: objectives,
		},
		[]string{"table"},
	)

	cacheMissLatency := promauto.With(registry).NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: namespace,
			Name:      "cache_miss_latency_ms",
			Help: "Reports on the write latency of the database, " +
				"but only measures the time to read a value when a cache miss occurs.",
			Objectives: objectives,
		},
		[]string{"table"},
	)

	bytesWrittenCounter := promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "bytes_written",
			Help:      "The number of bytes written to disk since startup. Only includes values, not metadata.",
		},
		[]string{"table"},
	)

	keysWrittenCounter := promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "keys_written",
			Help:      "The number of keys written to disk since startup.",
		},
		[]string{"table"},
	)

	writeLatency := promauto.With(registry).NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace:  namespace,
			Name:       "write_latency_ms",
			Help:       "Reports on the write latency of the database.",
			Objectives: objectives,
		},
		[]string{"table"},
	)

	flushCount := promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "flush_count",
			Help:      "The number of times a flush operation has been performed.",
		},
		[]string{"table"},
	)

	flushLatency := promauto.With(registry).NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace:  namespace,
			Name:       "flush_latency_ms",
			Help:       "Reports on the latency of a flush operation.",
			Objectives: objectives,
		},
		[]string{"table"},
	)

	segmentFlushLatency := promauto.With(registry).NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace:  namespace,
			Name:       "segment_flush_latency_ms",
			Help:       "Reports on segment flush latency. This is a subset of the time spent during a flush operation.",
			Objectives: objectives,
		},
		[]string{"table"},
	)

	keymapFlushLatency := promauto.With(registry).NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: namespace,
			Name:      "keymap_flush_latency_ms",
			Help: "Reports on the latency of a keymap flush operation. " +
				"This is a subset of the time spent during a flush operation.",
			Objectives: objectives,
		},
		[]string{"table"},
	)

	garbageCollectionLatency := promauto.With(registry).NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace:  namespace,
			Name:       "garbage_collection_latency_ms",
			Help:       "Reports on the latency of garbage collection operations.",
			Objectives: objectives,
		},
		[]string{"table"},
	)

	writeCacheMetrics := cache.NewCacheMetrics(
		registry,
		namespace,
		"chunk_write",
	)

	readCacheMetrics := cache.NewCacheMetrics(
		registry,
		namespace,
		"chunk_read",
	)

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
		garbageCollectionLatency: garbageCollectionLatency,
		segmentFlushLatency:      segmentFlushLatency,
		keymapFlushLatency:       keymapFlushLatency,
		writeCacheMetrics:        writeCacheMetrics,
		readCacheMetrics:         readCacheMetrics,
	}
}

// CollectPeriodicMetrics is a method that is periodically called to collect metrics. Tables are not permitted to be
// added or dropped while this method is running.
func (m *LittDBMetrics) CollectPeriodicMetrics(tables map[string]litt.ManagedTable) {
	if m == nil {
		return
	}

	for _, table := range tables {
		tableName := table.Name()

		tableSize := table.Size()
		m.tableSizeInBytes.WithLabelValues(tableName).Set(float64(tableSize))

		tableKeyCount := table.KeyCount()
		m.tableKeyCount.WithLabelValues(tableName).Set(float64(tableKeyCount))
	}
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

	m.bytesReadCounter.WithLabelValues(tableName).Add(float64(dataSize))
	m.keysReadCounter.WithLabelValues(tableName).Inc()
	m.readLatency.WithLabelValues(tableName).Observe(latency.Seconds())

	if cacheHit {
		m.cacheHitCounter.WithLabelValues(tableName).Inc()
	} else {
		m.cacheMissCounter.WithLabelValues(tableName).Inc()
		m.cacheMissLatency.WithLabelValues(tableName).Observe(common.ToMilliseconds(latency))
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

	m.bytesWrittenCounter.WithLabelValues(tableName).Add(float64(dataSize))
	m.keysWrittenCounter.WithLabelValues(tableName).Add(float64(batchSize))
	m.writeLatency.WithLabelValues(tableName).Observe(common.ToMilliseconds(latency))
}

// ReportFlushOperation reports the results of a flush operation.
func (m *LittDBMetrics) ReportFlushOperation(tableName string, latency time.Duration) {
	if m == nil {
		return
	}

	m.flushCount.WithLabelValues(tableName).Inc()
	m.flushLatency.WithLabelValues(tableName).Observe(common.ToMilliseconds(latency))
}

// ReportSegmentFlushLatency reports the amount of time taken to flush value files.
func (m *LittDBMetrics) ReportSegmentFlushLatency(tableName string, latency time.Duration) {
	if m == nil {
		return
	}

	m.segmentFlushLatency.WithLabelValues(tableName).Observe(common.ToMilliseconds(latency))
}

// ReportKeymapFlushLatency reports the amount of time taken to flush the keymap.
func (m *LittDBMetrics) ReportKeymapFlushLatency(tableName string, latency time.Duration) {
	if m == nil {
		return
	}

	m.keymapFlushLatency.WithLabelValues(tableName).Observe(common.ToMilliseconds(latency))
}

// ReportGarbageCollectionLatency reports the latency of a garbage collection operation.
func (m *LittDBMetrics) ReportGarbageCollectionLatency(tableName string, latency time.Duration) {
	if m == nil {
		return
	}

	m.garbageCollectionLatency.WithLabelValues(tableName).Observe(common.ToMilliseconds(latency))
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
