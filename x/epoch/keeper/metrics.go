package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("epoch_keeper")

	// finerGrainedBuckets units are in seconds
	finerGrainedBuckets = metric.WithExplicitBucketBoundaries(
		0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10,
	)

	epochMetrics = struct {
		beginBlockerDuration metric.Float64Histogram
		epochNew             metric.Int64Gauge
	}{
		beginBlockerDuration: must(meter.Float64Histogram(
			"epoch_begin_blocker_duration",
			metric.WithDescription("Duration of epoch begin-blocker execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		epochNew: must(meter.Int64Gauge(
			"epoch_new",
			metric.WithDescription("Current epoch number"),
			metric.WithUnit("{epoch}"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
