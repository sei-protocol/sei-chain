package walrussim

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

var meter = otel.Meter("walrussim")

// Instrument names carry their own unit and counter suffixes, so the name in the code is the name Prometheus
// scrapes. See the same note in the walrus package.

// The instruments the harness records. The engine records the read amplification itself; these describe the
// workload driving it and whether its answers were right.
var metrics = struct {
	BlocksWritten    metric.Int64Counter
	KeysWritten      metric.Int64Counter
	Reads            metric.Int64Counter
	ReadDuration     metric.Float64Histogram
	Mismatches       metric.Int64Counter
	SnapshotsTaken   metric.Int64Counter
	SnapshotDuration metric.Float64Histogram
	QueryableFirst   metric.Int64Gauge
	QueryableLast    metric.Int64Gauge
}{
	BlocksWritten: must(meter.Int64Counter(
		"walrussim_blocks_written_total",
		metric.WithDescription("Number of blocks handed to the engine"),
		metric.WithUnit("{count}"),
	)),
	KeysWritten: must(meter.Int64Counter(
		"walrussim_keys_written_total",
		metric.WithDescription("Number of key changes handed to the engine"),
		metric.WithUnit("{count}"),
	)),
	Reads: must(meter.Int64Counter(
		"walrussim_reads_total",
		metric.WithDescription("Number of historical reads, by the class of key asked for and how it resolved"),
		metric.WithUnit("{count}"),
	)),
	ReadDuration: must(meter.Float64Histogram(
		"walrussim_read_duration_seconds",
		metric.WithDescription("Time one historical read took, excluding the harness computing what to ask"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	)),
	Mismatches: must(meter.Int64Counter(
		"walrussim_mismatches_total",
		metric.WithDescription("Number of reads whose answer disagreed with the workload's own model"),
		metric.WithUnit("{count}"),
	)),
	SnapshotsTaken: must(meter.Int64Counter(
		"walrussim_snapshots_taken_total",
		metric.WithDescription("Number of checkpoints produced and retained"),
		metric.WithUnit("{count}"),
	)),
	SnapshotDuration: must(meter.Float64Histogram(
		"walrussim_snapshot_duration_seconds",
		metric.WithDescription("Time to checkpoint the state stub and retain the result"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LongLatencyBuckets...),
	)),
	QueryableFirst: must(meter.Int64Gauge(
		"walrussim_queryable_first_block",
		metric.WithDescription("Oldest block the engine can answer for"),
	)),
	QueryableLast: must(meter.Int64Gauge(
		"walrussim_queryable_last_block",
		metric.WithDescription("Newest block the engine can answer for"),
	)),
}

// must panics if instrument construction failed, which is a fault in the declaration above rather than a
// runtime condition a caller could handle.
func must[V any](instrument V, err error) V {
	if err != nil {
		panic(err)
	}
	return instrument
}

// recordBlockWritten records one block handed to the engine.
func recordBlockWritten(name string, keys int) {
	attrs := metric.WithAttributes(attribute.String("walrussim", name))
	ctx := context.Background()
	metrics.BlocksWritten.Add(ctx, 1, attrs)
	metrics.KeysWritten.Add(ctx, int64(keys), attrs)
}

// recordRead records one historical read.
//
// keyClass separates the never-written keys, which walk to the floor every time, from the ones a class
// writes. Averaging the two together would hide both.
func recordRead(name string, keyClass string, status string, elapsed time.Duration) {
	attrs := metric.WithAttributes(
		attribute.String("walrussim", name),
		attribute.String("key_class", keyClass),
		attribute.String("status", status),
	)
	ctx := context.Background()
	metrics.Reads.Add(ctx, 1, attrs)
	metrics.ReadDuration.Record(ctx, elapsed.Seconds(), attrs)
}

// recordMismatch records one answer that disagreed with the workload's model.
func recordMismatch(name string, keyClass string) {
	metrics.Mismatches.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("walrussim", name),
		attribute.String("key_class", keyClass),
	))
}

// recordSnapshot records one checkpoint produced and retained.
func recordSnapshot(name string, start time.Time) {
	attrs := metric.WithAttributes(attribute.String("walrussim", name))
	ctx := context.Background()
	metrics.SnapshotsTaken.Add(ctx, 1, attrs)
	metrics.SnapshotDuration.Record(ctx, time.Since(start).Seconds(), attrs)
}

// recordQueryable records the block range the engine can currently answer for.
func recordQueryable(name string, first uint64, last uint64) {
	attrs := metric.WithAttributes(attribute.String("walrussim", name))
	ctx := context.Background()
	//nolint:gosec // G115 - block numbers stay far below the int64 ceiling
	metrics.QueryableFirst.Record(ctx, int64(first), attrs)
	//nolint:gosec // G115 - as above
	metrics.QueryableLast.Record(ctx, int64(last), attrs)
}
