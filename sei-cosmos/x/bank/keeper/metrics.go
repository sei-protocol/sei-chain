package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_x_bank_keeper")

	bankMetrics = struct {
		sendAmount metric.Int64Gauge
	}{
		sendAmount: must(meter.Int64Gauge(
			"send_amount",
			metric.WithDescription("Amount sent in MsgSend transactions by denomination"),
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
