package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ibc_transfer_keeper")

	ibcTransferMetrics = struct {
		txMsgIbcTransfer         metric.Float64Gauge
		ibcTransferPacketReceive metric.Float64Gauge
		ibcTransferSend          metric.Int64Counter
		ibcTransferReceive       metric.Int64Counter
	}{
		txMsgIbcTransfer: must(meter.Float64Gauge(
			"ibc_transfer_tx_msg",
			metric.WithDescription("Total amount of tokens transferred via IBC"),
			metric.WithUnit("{token}"),
		)),
		ibcTransferPacketReceive: must(meter.Float64Gauge(
			"ibc_transfer_packet_receive",
			metric.WithDescription("Total amount of tokens received in IBC packet"),
			metric.WithUnit("{token}"),
		)),
		ibcTransferSend: must(meter.Int64Counter(
			"ibc_transfer_send",
			metric.WithDescription("Total number of IBC transfers sent"),
			metric.WithUnit("{count}"),
		)),
		ibcTransferReceive: must(meter.Int64Counter(
			"ibc_transfer_receive",
			metric.WithDescription("Total number of IBC transfers received"),
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
