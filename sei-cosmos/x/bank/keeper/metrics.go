package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_x_bank_keeper")

	bankMetrics = struct {
		sendAmount metric.Int64Gauge
		newAccount metric.Int64Counter
	}{
		sendAmount: must(meter.Int64Gauge(
			"last_send_amount",
			metric.WithDescription("Amount sent in the last MsgSend transaction by denomination"),
			metric.WithUnit("{utoken}"),
		)),
		newAccount: must(meter.Int64Counter(
			"bank_new_account",
			metric.WithDescription("Number of new accounts created during bank transfers"),
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
