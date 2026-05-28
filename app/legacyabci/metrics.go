package legacyabci

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("legacyabci")

	// finerGrainedBuckets units are in seconds
	finerGrainedBuckets = metric.WithExplicitBucketBoundaries(
		0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10,
	)

	legacyAbciMetrics = struct {
		totalBeginBlockDuration metric.Float64Histogram
		ibcBeginBlockerDuration metric.Float64Histogram
		txDuration              metric.Float64Histogram
	}{
		totalBeginBlockDuration: must(meter.Float64Histogram(
			"begin_blocker_duration",
			metric.WithDescription("Total duration of begin-block execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		ibcBeginBlockerDuration: must(meter.Float64Histogram(
			"ibc_begin_blocker_duration",
			metric.WithDescription("Duration of IBC begin-blocker execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		txDuration: must(meter.Float64Histogram(
			"tx_duration",
			metric.WithDescription("Duration of tx processing by mode (check, recheck, deliver)"),
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
