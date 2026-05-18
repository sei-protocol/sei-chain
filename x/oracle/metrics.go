package oracle

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("oracle")

	// finerGrainedBuckets units are in seconds
	finerGrainedBuckets = metric.WithExplicitBucketBoundaries(
		0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10,
	)

	oracleMetrics = struct {
		midBlockerDuration metric.Float64Histogram
		endBlockerDuration metric.Float64Histogram
		priceUpdateTotal   metric.Int64Counter
	}{
		midBlockerDuration: must(meter.Float64Histogram(
			"oracle_mid_blocker_duration",
			metric.WithDescription("Duration of oracle mid-blocker execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		endBlockerDuration: must(meter.Float64Histogram(
			"oracle_end_blocker_duration",
			metric.WithDescription("Duration of oracle end-blocker execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		priceUpdateTotal: must(meter.Int64Counter(
			"oracle_price_update",
			metric.WithDescription("Number of oracle price updates by denom"),
			metric.WithUnit("{count}"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
