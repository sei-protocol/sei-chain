package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_x_slashing_keeper")

	slashingKeeperMetrics = struct {
		validatorSlashed metric.Int64Counter
	}{
		validatorSlashed: must(meter.Int64Counter(
			"validator_slashed",
			metric.WithDescription("Number of validator slash events by type and validator address"),
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
