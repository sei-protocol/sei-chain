package pebblesim

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var meter = otel.Meter("pebblesim")

// simMetrics tracks how each batch performs against its block deadline, split into the full
// batch (key/value generation plus the Pebble write) and the Pebble write alone — so a slow
// batch can be attributed to the benchmark's own data generation versus Pebble itself.
type simMetrics struct {
	batchDuration  metric.Float64Histogram
	writeDuration  metric.Float64Histogram
	batchesWritten metric.Int64Counter
	slotsWritten   metric.Int64Counter
	deadlineMisses metric.Int64Counter
}

func newSimMetrics() *simMetrics {
	batchDuration, _ := meter.Float64Histogram(
		"pebblesim_batch_duration_seconds",
		metric.WithDescription("Wall-clock time for one full batch: key/value generation plus the Pebble write"),
		metric.WithUnit("s"),
	)
	writeDuration, _ := meter.Float64Histogram(
		"pebblesim_write_duration_seconds",
		metric.WithDescription("Wall-clock time for just the Pebble write within one batch (ApplyChangesetSync + SetLatestVersion)"),
		metric.WithUnit("s"),
	)
	batchesWritten, _ := meter.Int64Counter(
		"pebblesim_batches_written_total",
		metric.WithDescription("Total batches successfully written"),
	)
	slotsWritten, _ := meter.Int64Counter(
		"pebblesim_slots_written_total",
		metric.WithDescription("Total storage-slot key/value pairs successfully written"),
	)
	deadlineMisses, _ := meter.Int64Counter(
		"pebblesim_deadline_misses_total",
		metric.WithDescription("Batches whose total time exceeded the configured block interval"),
	)
	return &simMetrics{
		batchDuration:  batchDuration,
		writeDuration:  writeDuration,
		batchesWritten: batchesWritten,
		slotsWritten:   slotsWritten,
		deadlineMisses: deadlineMisses,
	}
}
