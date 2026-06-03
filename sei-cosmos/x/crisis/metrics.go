package crisis

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_x_crisis")

	// finerGrainedBuckets units are in seconds
	finerGrainedBuckets = metric.WithExplicitBucketBoundaries(
		0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10,
	)

	crisisMetrics = struct {
		endBlockerDuration           metric.Float64Histogram
		initGenesisUnmarshalDuration metric.Float64Histogram
	}{
		endBlockerDuration: must(meter.Float64Histogram(
			"crisis_end_blocker_duration",
			metric.WithDescription("Duration of crisis end-blocker execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		initGenesisUnmarshalDuration: must(meter.Float64Histogram(
			"crisis_init_genesis_unmarshal_duration",
			metric.WithDescription("Duration of crisis InitGenesis JSON unmarshal in seconds"),
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
