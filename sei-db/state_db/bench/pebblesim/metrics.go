package pebblesim

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

var meter = otel.Meter("pebblesim")

// batchLatencyBuckets is dense from 50ms to 2s around the default -interval block budget, then
// coarser out to 10s for the slow tail once compaction starts falling behind.
var batchLatencyBuckets = []float64{
	0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.4, 0.5,
	0.6, 0.75, 1, 1.5, 2, 3, 5, 7.5, 10,
}

// simMetrics tracks how each batch performs against its block deadline, split into the full
// batch, the Pebble write alone, and the stall waiting on the generator goroutine — so a slow
// batch can be attributed to Pebble itself versus the benchmark's own data generation falling
// behind.
type simMetrics struct {
	batchDuration  metric.Float64Histogram
	writeDuration  metric.Float64Histogram
	stallDuration  metric.Float64Histogram
	sortDuration   metric.Float64Histogram
	batchesWritten metric.Int64Counter
	keysWritten    metric.Int64Counter
	deadlineMisses metric.Int64Counter

	// readDuration and readErrors cover the read path (see PebbleSim.doRead). The underlying
	// pebbledb wrapper already emits pebble_get_latency on every Get, split by physical sub-DB —
	// readDuration exists only to add the hit/miss and kind split that layer can't give, not to
	// duplicate it.
	readDuration metric.Float64Histogram
	readErrors   metric.Int64Counter

	// latestHeight and earliestHeight track the store's retained version window, so pruning or
	// compaction behavior shows up directly as the gap between them changing over time.
	latestHeight   metric.Int64Gauge
	earliestHeight metric.Int64Gauge
}

func newSimMetrics() *simMetrics {
	batchDuration, _ := meter.Float64Histogram(
		"pebblesim_batch_duration_seconds",
		metric.WithDescription("Wall-clock time for one full batch: key/value generation plus the Pebble write"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(batchLatencyBuckets...),
	)
	writeDuration, _ := meter.Float64Histogram(
		"pebblesim_write_duration_seconds",
		metric.WithDescription("Wall-clock time for just the Pebble write within one batch (ApplyChangesetSync + SetLatestVersion)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(batchLatencyBuckets...),
	)
	stallDuration, _ := meter.Float64Histogram(
		"pebblesim_stall_duration_seconds",
		metric.WithDescription("Wall-clock time WriteBatch spent waiting for the generator goroutine to hand over the next batch; non-zero means generation, not Pebble, is the bottleneck"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(batchLatencyBuckets...),
	)
	sortDuration, _ := meter.Float64Histogram(
		"pebblesim_sort_duration_seconds",
		metric.WithDescription("Wall-clock time buildBatch spent sorting the batch by key on the generator goroutine; always 0 unless -presort is set"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(batchLatencyBuckets...),
	)
	batchesWritten, _ := meter.Int64Counter(
		"pebblesim_batches_written_total",
		metric.WithDescription("Total batches successfully written"),
	)
	keysWritten, _ := meter.Int64Counter(
		"pebblesim_keys_written_total",
		metric.WithDescription("Total key/value pairs successfully written, labeled by kind (slot, balance, nonce)"),
	)
	deadlineMisses, _ := meter.Int64Counter(
		"pebblesim_deadline_misses_total",
		metric.WithDescription("Batches whose total time exceeded the configured block interval"),
	)
	readDuration, _ := meter.Float64Histogram(
		"pebblesim_read_duration_seconds",
		metric.WithDescription("Wall-clock time for a single random-read Get, labeled by kind and hit/miss"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(metrics.LatencyBuckets...),
	)
	readErrors, _ := meter.Int64Counter(
		"pebblesim_read_errors_total",
		metric.WithDescription("Random reads that returned an error (not counting not-found, which is a miss, not an error)"),
	)
	latestHeight, _ := meter.Int64Gauge(
		"pebblesim_latest_height",
		metric.WithDescription("Most recently written version"),
	)
	earliestHeight, _ := meter.Int64Gauge(
		"pebblesim_earliest_height",
		metric.WithDescription("Oldest version still retained by the store"),
	)
	return &simMetrics{
		batchDuration:  batchDuration,
		writeDuration:  writeDuration,
		stallDuration:  stallDuration,
		sortDuration:   sortDuration,
		batchesWritten: batchesWritten,
		keysWritten:    keysWritten,
		deadlineMisses: deadlineMisses,
		readDuration:   readDuration,
		readErrors:     readErrors,
		latestHeight:   latestHeight,
		earliestHeight: earliestHeight,
	}
}
