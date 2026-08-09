package composite

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

var snapshotMeter = otel.Meter("seidb_ss_snapshot")

var snapshotMetrics = struct {
	Attempts      metric.Int64Counter
	Skipped       metric.Int64Counter
	Completions   metric.Int64Counter
	Duration      metric.Float64Histogram
	InFlight      metric.Int64Gauge
	CurrentHeight metric.Int64Gauge
	RetainedCount metric.Int64Gauge
	ApparentBytes metric.Int64Gauge
}{
	Attempts: must(snapshotMeter.Int64Counter(
		"ss_snapshot_attempts",
		metric.WithDescription("Number of state-store snapshot attempts"),
		metric.WithUnit("{count}"),
	)),
	Skipped: must(snapshotMeter.Int64Counter(
		"ss_snapshot_skipped",
		metric.WithDescription("Number of state-store snapshot boundaries skipped by a scheduling gate"),
		metric.WithUnit("{count}"),
	)),
	Completions: must(snapshotMeter.Int64Counter(
		"ss_snapshot_completions",
		metric.WithDescription("Number of completed state-store snapshot attempts"),
		metric.WithUnit("{count}"),
	)),
	Duration: must(snapshotMeter.Float64Histogram(
		"ss_snapshot_duration",
		metric.WithDescription("Time from a state-store snapshot request to completion"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LongLatencyBuckets...),
	)),
	InFlight: must(snapshotMeter.Int64Gauge(
		"ss_snapshot_in_flight",
		metric.WithDescription("Whether one state-store snapshot is currently in flight"),
	)),
	CurrentHeight: must(snapshotMeter.Int64Gauge(
		"ss_snapshot_current_height",
		metric.WithDescription("Height of the newest published state-store snapshot"),
	)),
	RetainedCount: must(snapshotMeter.Int64Gauge(
		"ss_snapshot_retained_count",
		metric.WithDescription("Number of retained state-store snapshots"),
		metric.WithUnit("{count}"),
	)),
	ApparentBytes: must(snapshotMeter.Int64Gauge(
		"ss_snapshot_retained_apparent_bytes",
		metric.WithDescription("Apparent bytes referenced by retained state-store snapshots; hardlinks can share physical blocks"),
		metric.WithUnit("By"),
	)),
}

func must[V any](instrument V, err error) V {
	if err != nil {
		panic(err)
	}
	return instrument
}

func recordSnapshotAttempt() {
	snapshotMetrics.Attempts.Add(context.Background(), 1)
}

func recordSnapshotSkipped(reason string) {
	snapshotMetrics.Skipped.Add(
		context.Background(),
		1,
		metric.WithAttributes(attribute.String("reason", reason)),
	)
}

func recordSnapshotInFlight(value int64) {
	snapshotMetrics.InFlight.Record(context.Background(), value)
}

func recordSnapshotCompletion(start time.Time, outcome string) {
	attrs := metric.WithAttributes(attribute.String("outcome", outcome))
	snapshotMetrics.Completions.Add(context.Background(), 1, attrs)
	snapshotMetrics.Duration.Record(context.Background(), time.Since(start).Seconds(), attrs)
}
