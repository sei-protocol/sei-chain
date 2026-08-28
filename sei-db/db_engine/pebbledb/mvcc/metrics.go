package mvcc

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seidb_pebble")

	// otelMetrics holds MVCC operation-level instruments, shared across every
	// Database instance in the process; each Record call attaches a "db"
	// attribute for the specific instance. Pebble-internal stats (compaction,
	// flush, sstable, memtable, WAL, cache) are reported separately, once per
	// instance, by pebbledb.PebbleMetrics.
	otelMetrics = struct {
		getLatency                 metric.Float64Histogram
		applyChangesetLatency      metric.Float64Histogram
		applyChangesetAsyncLatency metric.Float64Histogram
		pruneLatency               metric.Float64Histogram
		pruneConsecutiveFailures   metric.Int64Gauge
		importLatency              metric.Float64Histogram
		batchWriteLatency          metric.Float64Histogram

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
		pruneConsecutiveFailures: must(meter.Int64Gauge(
			"pebble_prune_consecutive_failures",
			metric.WithDescription(
				"Prune passes that have failed in a row. Every pass raises the earliest-version marker "+
					"before it deletes, so a rising value means the served history window is narrowing "+
					"once per prune interval while nothing leaves disk",
			),
			metric.WithUnit("{count}"),
		)),
		importLatency: must(meter.Float64Histogram(
			"pebble_import_latency",
			metric.WithDescription("Time taken to import snapshot data to PebbleDB"),
			metric.WithUnit("s"),
		)),
		batchWriteLatency: must(meter.Float64Histogram(
			"pebble_batch_write_latency",
			metric.WithDescription("Time taken to write a batch to PebbleDB"),
			metric.WithUnit("s"),
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
