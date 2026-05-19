package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_x_upgrade_keeper")

	upgradeKeeperMetrics = struct {
		planHeight metric.Int64Gauge
	}{
		planHeight: must(meter.Int64Gauge(
			"plan_height",
			metric.WithDescription("Scheduled upgrade plan height by name and info"),
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
