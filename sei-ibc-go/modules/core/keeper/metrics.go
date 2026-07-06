package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ibc_core")

	ibcCoreMetrics = struct {
		txMsgIbcRecvPacket        metric.Int64Counter
		ibcTimeoutPacket          metric.Int64Counter
		txMsgIbcAcknowledgePacket metric.Int64Counter
	}{
		txMsgIbcRecvPacket: must(meter.Int64Counter(
			"ibc_core_tx_msg_recv_packet",
			metric.WithDescription("Total number of IBC recv packet messages"),
			metric.WithUnit("{count}"),
		)),
		ibcTimeoutPacket: must(meter.Int64Counter(
			"ibc_core_timeout_packet",
			metric.WithDescription("Total number of IBC timeout packets"),
			metric.WithUnit("{count}"),
		)),
		txMsgIbcAcknowledgePacket: must(meter.Int64Counter(
			"ibc_core_tx_msg_acknowledge_packet",
			metric.WithDescription("Total number of IBC acknowledge packet messages"),
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
