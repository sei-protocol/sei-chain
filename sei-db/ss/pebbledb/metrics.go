package pebbledb

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seidb.pebble")

	// Metrics contains all PebbleDB-specific metrics
	Metrics = struct {
		// Operation Latencies
		GetLatency                 metric.Float64Histogram
		ApplyChangesetLatency      metric.Float64Histogram
		ApplyChangesetAsyncLatency metric.Float64Histogram
		PruneLatency               metric.Float64Histogram
		ImportLatency              metric.Float64Histogram
		HashComputationLatency     metric.Float64Histogram
		BatchWriteLatency          metric.Float64Histogram

		// Database Internal Metrics
		CompactionCount        metric.Int64Counter
		CompactionLatency      metric.Float64Histogram
		CompactionBytesRead    metric.Int64Counter
		CompactionBytesWritten metric.Int64Counter
		FlushCount             metric.Int64Counter
		FlushLatency           metric.Float64Histogram
		FlushBytesWritten      metric.Int64Counter

		// Storage Metrics
		SSTableCount      metric.Int64Gauge
		SSTableTotalSize  metric.Int64Gauge
		MemtableCount     metric.Int64Gauge
		MemtableTotalSize metric.Int64Gauge
		WALSize           metric.Int64Gauge

		// Cache Metrics
		CacheHits   metric.Int64Counter
		CacheMisses metric.Int64Counter
		CacheSize   metric.Int64Gauge

		// Operational Metrics
		BatchSize                    metric.Int64Histogram
		PendingChangesQueueDepth     metric.Int64Gauge
		IteratorCount                metric.Int64UpDownCounter
		IteratorCreationCount        metric.Int64Counter
		ReverseIteratorCreationCount metric.Int64Counter
		IteratorNextCallCount        metric.Int64Counter
		IteratorPrevCallCount        metric.Int64Counter
	}{
		// Operation Latencies
		GetLatency: must(meter.Float64Histogram(
			"pebble_get_latency",
			metric.WithDescription("Time taken to get a key from PebbleDB"),
			metric.WithUnit("s"),
		)),
		ApplyChangesetLatency: must(meter.Float64Histogram(
			"pebble_apply_changeset_latency",
			metric.WithDescription("Time taken to apply changeset to PebbleDB"),
			metric.WithUnit("s"),
		)),
		ApplyChangesetAsyncLatency: must(meter.Float64Histogram(
			"pebble_apply_changeset_async_latency",
			metric.WithDescription("Time taken to queue changeset for async write"),
			metric.WithUnit("s"),
		)),
		PruneLatency: must(meter.Float64Histogram(
			"pebble_prune_latency",
			metric.WithDescription("Time taken to prune old versions from PebbleDB"),
			metric.WithUnit("s"),
		)),
		ImportLatency: must(meter.Float64Histogram(
			"pebble_import_latency",
			metric.WithDescription("Time taken to import snapshot data to PebbleDB"),
			metric.WithUnit("s"),
		)),
		HashComputationLatency: must(meter.Float64Histogram(
			"pebble_hash_computation_latency",
			metric.WithDescription("Time taken to compute hash for a block range"),
			metric.WithUnit("s"),
		)),
		BatchWriteLatency: must(meter.Float64Histogram(
			"pebble_batch_write_latency",
			metric.WithDescription("Time taken to write a batch to PebbleDB"),
			metric.WithUnit("s"),
		)),

		// Compaction Metrics
		CompactionCount: must(meter.Int64Counter(
			"pebble_compaction_count",
			metric.WithDescription("Total number of compactions"),
			metric.WithUnit("{count}"),
		)),
		CompactionLatency: must(meter.Float64Histogram(
			"pebble_compaction_latency",
			metric.WithDescription("Latency of compaction operations"),
			metric.WithUnit("s"),
		)),
		CompactionBytesRead: must(meter.Int64Counter(
			"pebble_compaction_bytes_read",
			metric.WithDescription("Total bytes read during compaction"),
			metric.WithUnit("By"),
		)),
		CompactionBytesWritten: must(meter.Int64Counter(
			"pebble_compaction_bytes_written",
			metric.WithDescription("Total bytes written during compaction"),
			metric.WithUnit("By"),
		)),

		// Flush Metrics
		FlushCount: must(meter.Int64Counter(
			"pebble_flush_count",
			metric.WithDescription("Total number of memtable flushes"),
			metric.WithUnit("{count}"),
		)),
		FlushLatency: must(meter.Float64Histogram(
			"pebble_flush_latency",
			metric.WithDescription("Latency of memtable flush operations"),
			metric.WithUnit("s"),
		)),
		FlushBytesWritten: must(meter.Int64Counter(
			"pebble_flush_bytes_written",
			metric.WithDescription("Total bytes written during memtable flushes"),
			metric.WithUnit("By"),
		)),

		// Storage Metrics
		SSTableCount: must(meter.Int64Gauge(
			"pebble_sstable_count",
			metric.WithDescription("Current number of SSTables"),
			metric.WithUnit("{count}"),
		)),
		SSTableTotalSize: must(meter.Int64Gauge(
			"pebble_sstable_total_size",
			metric.WithDescription("Total size of all SSTables"),
			metric.WithUnit("By"),
		)),
		MemtableCount: must(meter.Int64Gauge(
			"pebble_memtable_count",
			metric.WithDescription("Current number of memtables"),
			metric.WithUnit("{count}"),
		)),
		MemtableTotalSize: must(meter.Int64Gauge(
			"pebble_memtable_total_size",
			metric.WithDescription("Total size of all memtables"),
			metric.WithUnit("By"),
		)),
		WALSize: must(meter.Int64Gauge(
			"pebble_wal_size",
			metric.WithDescription("Current size of Write-Ahead Log"),
			metric.WithUnit("By"),
		)),

		// Cache Metrics
		CacheHits: must(meter.Int64Counter(
			"pebble_cache_hits",
			metric.WithDescription("Total number of cache hits"),
			metric.WithUnit("{count}"),
		)),
		CacheMisses: must(meter.Int64Counter(
			"pebble_cache_misses",
			metric.WithDescription("Total number of cache misses"),
			metric.WithUnit("{count}"),
		)),
		CacheSize: must(meter.Int64Gauge(
			"pebble_cache_size",
			metric.WithDescription("Current cache size"),
			metric.WithUnit("By"),
		)),

		// Operational Metrics
		BatchSize: must(meter.Int64Histogram(
			"pebble_batch_size",
			metric.WithDescription("Size of batches written to PebbleDB"),
			metric.WithUnit("By"),
		)),
		PendingChangesQueueDepth: must(meter.Int64Gauge(
			"pebble_pending_changes_queue_depth",
			metric.WithDescription("Number of pending changesets in async write queue"),
			metric.WithUnit("{count}"),
		)),
		IteratorCount: must(meter.Int64UpDownCounter(
			"pebble_iterator_count",
			metric.WithDescription("Current number of active (not yet closed) iterators"),
			metric.WithUnit("{count}"),
		)),
		IteratorCreationCount: must(meter.Int64Counter(
			"pebble_iterator_creation_count",
			metric.WithDescription("Total number of forward iterators created"),
			metric.WithUnit("{count}"),
		)),
		ReverseIteratorCreationCount: must(meter.Int64Counter(
			"pebble_reverse_iterator_creation_count",
			metric.WithDescription("Total number of reverse iterators created"),
			metric.WithUnit("{count}"),
		)),
		IteratorNextCallCount: must(meter.Int64Counter(
			"pebble_iterator_next_call_count",
			metric.WithDescription("Total number of iterator Next calls (forward iteration)"),
			metric.WithUnit("{count}"),
		)),
		IteratorPrevCallCount: must(meter.Int64Counter(
			"pebble_iterator_prev_call_count",
			metric.WithDescription("Total number of iterator Prev calls (reverse iteration)"),
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
