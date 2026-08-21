package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ibc_transfer_keeper")

	ibcTransferMetrics = struct {
		txMsgIbcTransfer metric.Int64Gauge
		ibcTransferSend  metric.Int64Counter
	}{
		txMsgIbcTransfer: must(meter.Int64Gauge(
			"ibc_transfer_tx_msg",
			metric.WithDescription("Last amount of tokens transferred via IBC per denom class"),
			metric.WithUnit("{token}"),
		)),
		ibcTransferSend: must(meter.Int64Counter(
			"ibc_transfer_send",
			metric.WithDescription("Total number of IBC transfers sent"),
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
