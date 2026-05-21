package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_x_distribution_keeper")

	distributionMetrics = struct {
		withdrawRewardAmount     metric.Int64Gauge
		withdrawCommissionAmount metric.Int64Gauge
	}{
		withdrawRewardAmount: must(meter.Int64Gauge(
			"withdraw_reward_amount",
			metric.WithDescription("Amount withdrawn as delegation rewards in the last withdrawal transaction by denomination"),
			metric.WithUnit("{utoken}"),
		)),
		withdrawCommissionAmount: must(meter.Int64Gauge(
			"withdraw_commission_amount",
			metric.WithDescription("Amount withdrawn as validator commission in the last withdrawal transaction by denomination"),
			metric.WithUnit("{utoken}"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
