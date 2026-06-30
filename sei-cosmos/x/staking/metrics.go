package staking

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_x_staking")

	// finerGrainedBuckets units are in seconds
	finerGrainedBuckets = metric.WithExplicitBucketBoundaries(
		0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10,
	)

	stakingMetrics = struct {
		beginBlockerDuration metric.Float64Histogram
		endBlockerDuration   metric.Float64Histogram
	}{
		beginBlockerDuration: must(meter.Float64Histogram(
			"staking_begin_blocker_duration",
			metric.WithDescription("Duration of staking begin-blocker execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		endBlockerDuration: must(meter.Float64Histogram(
			"staking_end_blocker_duration",
			metric.WithDescription("Duration of staking end-blocker execution in seconds"),
			finerGrainedBuckets,
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
