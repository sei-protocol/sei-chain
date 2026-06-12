package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_x_gov_keeper")

	govMetrics = struct {
		proposalTotal metric.Int64Counter
		voteTotal     metric.Int64Counter
		depositTotal  metric.Int64Counter
	}{
		proposalTotal: must(meter.Int64Counter(
			"proposal",
			metric.WithDescription("Number of governance proposals submitted"),
			metric.WithUnit("{count}"),
		)),
		voteTotal: must(meter.Int64Counter(
			"vote",
			metric.WithDescription("Number of governance votes cast by proposal"),
			metric.WithUnit("{count}"),
		)),
		depositTotal: must(meter.Int64Counter(
			"deposit",
			metric.WithDescription("Number of governance deposits made by proposal"),
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
