package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_x_staking_keeper")

	stakingMetrics = struct {
		delegateTotal    metric.Int64Counter
		delegateAmount   metric.Int64Gauge
		redelegateTotal  metric.Int64Counter
		redelegateAmount metric.Int64Gauge
		undelegateTotal  metric.Int64Counter
		undelegateAmount metric.Int64Gauge
	}{
		delegateTotal: must(meter.Int64Counter(
			"staking_keeper_delegate",
			metric.WithDescription("Number of delegation transactions"),
			metric.WithUnit("{count}"),
		)),
		delegateAmount: must(meter.Int64Gauge(
			"staking_keeper_last_delegate_amount",
			metric.WithDescription("Amount delegated in the last delegation transaction"),
			metric.WithUnit("{usei}"),
		)),
		redelegateTotal: must(meter.Int64Counter(
			"staking_keeper_redelegate",
			metric.WithDescription("Number of redelegation transactions"),
			metric.WithUnit("{count}"),
		)),
		redelegateAmount: must(meter.Int64Gauge(
			"staking_keeper_last_redelegate_amount",
			metric.WithDescription("Amount redelegated in the last redelegation transaction"),
			metric.WithUnit("{usei}"),
		)),
		undelegateTotal: must(meter.Int64Counter(
			"staking_keeper_undelegate",
			metric.WithDescription("Number of undelegation transactions"),
			metric.WithUnit("{count}"),
		)),
		undelegateAmount: must(meter.Int64Gauge(
			"staking_keeper_last_undelegate_amount",
			metric.WithDescription("Amount undelegated in the last undelegation transaction"),
			metric.WithUnit("{usei}"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
