package pebbledb

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seidb_pebble")

	otelMetrics = struct {
		getLatency                 metric.Float64Histogram
		applyChangesetLatency      metric.Float64Histogram
		applyChangesetAsyncLatency metric.Float64Histogram
		pruneLatency               metric.Float64Histogram
		importLatency              metric.Float64Histogram
		hashComputationLatency     metric.Float64Histogram
		batchWriteLatency          metric.Float64Histogram

		compactionCount        metric.Int64Counter
		compactionDuration     metric.Float64Histogram
		compactionBytesRead    metric.Int64Counter
		compactionBytesWritten metric.Int64Counter
		flushCount             metric.Int64Counter
		flushDuration          metric.Float64Histogram
		flushBytesWritten      metric.Int64Counter

		sstableCount      metric.Int64Gauge
		sstableTotalSize  metric.Int64Gauge
		memtableCount     metric.Int64Gauge
		memtableTotalSize metric.Int64Gauge
		walSize           metric.Int64Gauge

		cacheHits   metric.Int64Counter
		cacheMisses metric.Int64Counter
		cacheSize   metric.Int64Gauge

		batchSize                metric.Int64Histogram
		pendingChangesQueueDepth metric.Int64Gauge
		iteratorIterations       metric.Float64Histogram
	}{
		getLatency: must(meter.Float64Histogram(
			"pebble_get_latency",
			metric.WithDescription("Time taken to get a key from PebbleDB"),
			metric.WithUnit("s"),
		)),
		applyChangesetLatency: must(meter.Float64Histogram(
			"pebble_apply_changeset_latency",
			metric.WithDescription("Time taken to apply changeset to PebbleDB"),
			metric.WithUnit("s"),
		)),
		applyChangesetAsyncLatency: must(meter.Float64Histogram(
			"pebble_apply_changeset_async_latency",
			metric.WithDescription("Time taken to queue changeset for async write"),
			metric.WithUnit("s"),
		)),
		pruneLatency: must(meter.Float64Histogram(
			"pebble_prune_latency",
			metric.WithDescription("Time taken to prune old versions from PebbleDB"),
			metric.WithUnit("s"),
		)),
		importLatency: must(meter.Float64Histogram(
			"pebble_import_latency",
			metric.WithDescription("Time taken to import snapshot data to PebbleDB"),
			metric.WithUnit("s"),
		)),
		hashComputationLatency: must(meter.Float64Histogram(
			"pebble_hash_computation_latency",
			metric.WithDescription("Time taken to compute hash for a block range"),
			metric.WithUnit("s"),
		)),
		batchWriteLatency: must(meter.Float64Histogram(
			"pebble_batch_write_latency",
			metric.WithDescription("Time taken to write a batch to PebbleDB"),
			metric.WithUnit("s"),
		)),

		compactionCount: must(meter.Int64Counter(
			"pebble_compaction_count",
			metric.WithDescription("Total number of compactions"),
			metric.WithUnit("{count}"),
		)),
		compactionDuration: must(meter.Float64Histogram(
			"pebble_compaction_duration",
			metric.WithDescription("Duration of compaction operations"),
			metric.WithUnit("s"),
		)),
		compactionBytesRead: must(meter.Int64Counter(
			"pebble_compaction_bytes_read",
			metric.WithDescription("Total bytes read during compaction"),
			metric.WithUnit("By"),
		)),
		compactionBytesWritten: must(meter.Int64Counter(
			"pebble_compaction_bytes_written",
			metric.WithDescription("Total bytes written during compaction"),
			metric.WithUnit("By"),
		)),

		flushCount: must(meter.Int64Counter(
			"pebble_flush_count",
			metric.WithDescription("Total number of memtable flushes"),
			metric.WithUnit("{count}"),
		)),
		flushDuration: must(meter.Float64Histogram(
			"pebble_flush_duration",
			metric.WithDescription("Duration of memtable flush operations"),
			metric.WithUnit("s"),
		)),
		flushBytesWritten: must(meter.Int64Counter(
			"pebble_flush_bytes_written",
			metric.WithDescription("Total bytes written during memtable flushes"),
			metric.WithUnit("By"),
		)),

		sstableCount: must(meter.Int64Gauge(
			"pebble_sstable_count",
			metric.WithDescription("Current number of SSTables at each level"),
			metric.WithUnit("{count}"),
		)),
		sstableTotalSize: must(meter.Int64Gauge(
			"pebble_sstable_total_size",
			metric.WithDescription("Total size of SSTables at each level"),
			metric.WithUnit("By"),
		)),
		memtableCount: must(meter.Int64Gauge(
			"pebble_memtable_count",
			metric.WithDescription("Current number of memtables"),
			metric.WithUnit("{count}"),
		)),
		memtableTotalSize: must(meter.Int64Gauge(
			"pebble_memtable_total_size",
			metric.WithDescription("Total size of all memtables"),
			metric.WithUnit("By"),
		)),
		walSize: must(meter.Int64Gauge(
			"pebble_wal_size",
			metric.WithDescription("Current size of Write-Ahead Log"),
			metric.WithUnit("By"),
		)),

		cacheHits: must(meter.Int64Counter(
			"pebble_cache_hits",
			metric.WithDescription("Total number of cache hits"),
			metric.WithUnit("{count}"),
		)),
		cacheMisses: must(meter.Int64Counter(
			"pebble_cache_misses",
			metric.WithDescription("Total number of cache misses"),
			metric.WithUnit("{count}"),
		)),
		cacheSize: must(meter.Int64Gauge(
			"pebble_cache_size",
			metric.WithDescription("Current cache size"),
			metric.WithUnit("By"),
		)),

		batchSize: must(meter.Int64Histogram(
			"pebble_batch_size",
			metric.WithDescription("Size of batches written to PebbleDB"),
			metric.WithUnit("By"),
		)),
		pendingChangesQueueDepth: must(meter.Int64Gauge(
			"pebble_pending_changes_queue_depth",
			metric.WithDescription("Number of pending changesets in async write queue"),
			metric.WithUnit("{count}"),
		)),
		iteratorIterations: must(meter.Float64Histogram(
			"pebble_iterator_iterations",
			metric.WithDescription("Number of iterations per iterator"),
			metric.WithUnit("{count}"),
		)),
	}
)

// must panics if err is non-nil, otherwise returns v.
func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
