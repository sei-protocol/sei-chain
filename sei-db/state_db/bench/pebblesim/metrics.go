package pebblesim

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var meter = otel.Meter("pebblesim")

// batchLatencyBuckets is dense from 50ms to 2s around the default -interval block budget, then
// coarser out to 10s for the slow tail once compaction starts falling behind.
var batchLatencyBuckets = []float64{
	0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.4, 0.5,
	0.6, 0.75, 1, 1.5, 2, 3, 5, 7.5, 10,
}

// simMetrics tracks how each batch performs against its block deadline, split into the full
// batch (key/value generation plus the Pebble write) and the Pebble write alone — so a slow
// batch can be attributed to the benchmark's own data generation versus Pebble itself.
type simMetrics struct {
	batchDuration  metric.Float64Histogram
	writeDuration  metric.Float64Histogram
	batchesWritten metric.Int64Counter
	keysWritten    metric.Int64Counter
	deadlineMisses metric.Int64Counter
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
	return &simMetrics{
		batchDuration:  batchDuration,
		writeDuration:  writeDuration,
		batchesWritten: batchesWritten,
		keysWritten:    keysWritten,
		deadlineMisses: deadlineMisses,
	}
}
