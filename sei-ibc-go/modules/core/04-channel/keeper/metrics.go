package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ibc_channel")

	ibcChannelMetrics = struct {
		ibcChannelOpenInit     metric.Int64Counter
		ibcChannelOpenTry      metric.Int64Counter
		ibcChannelOpenAck      metric.Int64Counter
		ibcChannelOpenConfirm  metric.Int64Counter
		ibcChannelCloseInit    metric.Int64Counter
		ibcChannelCloseConfirm metric.Int64Counter
	}{
		ibcChannelOpenInit: must(meter.Int64Counter(
			"ibc_channel_open_init",
			metric.WithDescription("Total number of IBC channel open-init handshakes"),
			metric.WithUnit("{count}"),
		)),
		ibcChannelOpenTry: must(meter.Int64Counter(
			"ibc_channel_open_try",
			metric.WithDescription("Total number of IBC channel open-try handshakes"),
			metric.WithUnit("{count}"),
		)),
		ibcChannelOpenAck: must(meter.Int64Counter(
			"ibc_channel_open_ack",
			metric.WithDescription("Total number of IBC channel open-ack handshakes"),
			metric.WithUnit("{count}"),
		)),
		ibcChannelOpenConfirm: must(meter.Int64Counter(
			"ibc_channel_open_confirm",
			metric.WithDescription("Total number of IBC channel open-confirm handshakes"),
			metric.WithUnit("{count}"),
		)),
		ibcChannelCloseInit: must(meter.Int64Counter(
			"ibc_channel_close_init",
			metric.WithDescription("Total number of IBC channel close-init handshakes"),
			metric.WithUnit("{count}"),
		)),
		ibcChannelCloseConfirm: must(meter.Int64Counter(
			"ibc_channel_close_confirm",
			metric.WithDescription("Total number of IBC channel close-confirm handshakes"),
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
