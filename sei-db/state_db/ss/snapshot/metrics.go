package snapshot

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

var meter = otel.Meter("seidb_ss_snapshot")

var metrics = struct {
	Attempts      metric.Int64Counter
	Skipped       metric.Int64Counter
	Completions   metric.Int64Counter
	Duration      metric.Float64Histogram
	InFlight      metric.Int64Gauge
	CurrentHeight metric.Int64Gauge
	CommonHeight  metric.Int64Gauge
	RetainedCount metric.Int64Gauge
	ApparentBytes metric.Int64Gauge
}{
	Attempts: must(meter.Int64Counter(
		"ss_snapshot_attempts",
		metric.WithDescription("Number of state-store snapshot attempts"),
		metric.WithUnit("{count}"),
	)),
	Skipped: must(meter.Int64Counter(
		"ss_snapshot_skipped",
		metric.WithDescription("Number of state-store snapshot boundaries skipped by a scheduling gate"),
		metric.WithUnit("{count}"),
	)),
	Completions: must(meter.Int64Counter(
		"ss_snapshot_completions",
		metric.WithDescription("Number of completed state-store snapshot attempts"),
		metric.WithUnit("{count}"),
	)),
	Duration: must(meter.Float64Histogram(
		"ss_snapshot_duration",
		metric.WithDescription("Time from a state-store snapshot request to completion"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LongLatencyBuckets...),
	)),
	InFlight: must(meter.Int64Gauge(
		"ss_snapshot_in_flight",
		metric.WithDescription("Whether one state-store snapshot is currently in flight"),
	)),
	CurrentHeight: must(meter.Int64Gauge(
		"ss_snapshot_current_height",
		metric.WithDescription("Height of the newest published state-store snapshot"),
	)),
	CommonHeight: must(meter.Int64Gauge(
		"ss_snapshot_common_height",
		metric.WithDescription("Newest state-store snapshot height present in every enabled SS member"),
	)),
	RetainedCount: must(meter.Int64Gauge(
		"ss_snapshot_retained_count",
		metric.WithDescription("Number of retained state-store snapshots"),
		metric.WithUnit("{count}"),
	)),
	ApparentBytes: must(meter.Int64Gauge(
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

func RecordAttempt() {
	metrics.Attempts.Add(context.Background(), 1)
}

func RecordSkipped(reason string) {
	metrics.Skipped.Add(
		context.Background(),
		1,
		metric.WithAttributes(attribute.String("reason", reason)),
	)
}

func RecordInFlight(value int64) {
	metrics.InFlight.Record(context.Background(), value)
}

func RecordCompletion(start time.Time, outcome string) {
	attrs := metric.WithAttributes(attribute.String("outcome", outcome))
	metrics.Completions.Add(context.Background(), 1, attrs)
	metrics.Duration.Record(context.Background(), time.Since(start).Seconds(), attrs)
}

func RecordCommonHeight(height int64) {
	metrics.CommonHeight.Record(context.Background(), height)
}

func recordCurrentHeight(store string, height int64) {
	metrics.CurrentHeight.Record(
		context.Background(),
		height,
		metric.WithAttributes(attribute.String("store", store)),
	)
}

func recordRetainedCount(store string, count int64) {
	metrics.RetainedCount.Record(
		context.Background(),
		count,
		metric.WithAttributes(attribute.String("store", store)),
	)
}

func recordApparentBytes(store string, bytes int64) {
	metrics.ApparentBytes.Record(
		context.Background(),
		bytes,
		metric.WithAttributes(attribute.String("store", store)),
	)
}
