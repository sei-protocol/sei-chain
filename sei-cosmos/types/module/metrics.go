package module

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_types_module")

	// midBlockDurationBuckets units are in seconds
	midBlockDurationBuckets = metric.WithExplicitBucketBoundaries(
		0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10,
	)

	moduleMetrics = struct {
		totalMidBlockDuration metric.Float64Histogram
		midBlockDuration      metric.Float64Histogram
	}{
		totalMidBlockDuration: must(meter.Float64Histogram(
			"module_total_mid_block_duration",
			metric.WithDescription("Total duration of all modules' mid-block execution in seconds"),
			midBlockDurationBuckets,
			metric.WithUnit("s"),
		)),
		midBlockDuration: must(meter.Float64Histogram(
			"module_mid_block_duration",
			metric.WithDescription("Duration of per-module mid-block execution in seconds"),
			midBlockDurationBuckets,
			metric.WithUnit("s"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
