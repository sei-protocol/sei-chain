package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_x_upgrade_keeper")

	upgradeKeeperMetrics = struct {
		pendingPlanHeight metric.Int64Gauge
	}{
		pendingPlanHeight: must(meter.Int64Gauge(
			"upgrade_pending_plan_height",
			metric.WithDescription("Height of a pending upgrade plan at schedule time by name"),
			metric.WithUnit("{block}"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
