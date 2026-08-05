package gaskv

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_store_gaskv")

	// latencyBuckets units are in seconds.
	latencyBuckets = metric.WithExplicitBucketBoundaries(
		0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10,
	)

	gaskvMetrics = struct {
		hasDuration    metric.Float64Histogram
		deleteDuration metric.Float64Histogram
	}{
		hasDuration: must(meter.Float64Histogram(
			"gaskv_has_duration",
			metric.WithDescription("Duration of gaskv Has operations in seconds"),
			latencyBuckets,
			metric.WithUnit("s"),
		)),
		deleteDuration: must(meter.Float64Histogram(
			"gaskv_delete_duration",
			metric.WithDescription("Duration of gaskv Delete operations in seconds"),
			latencyBuckets,
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
