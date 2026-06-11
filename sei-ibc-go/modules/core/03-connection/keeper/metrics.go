package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ibc_connection")

	ibcConnectionMetrics = struct {
		ibcConnectionOpenInit    metric.Int64Counter
		ibcConnectionOpenTry     metric.Int64Counter
		ibcConnectionOpenAck     metric.Int64Counter
		ibcConnectionOpenConfirm metric.Int64Counter
	}{
		ibcConnectionOpenInit: must(meter.Int64Counter(
			"ibc_connection_open_init",
			metric.WithDescription("Total number of IBC connection open-init handshakes"),
			metric.WithUnit("{count}"),
		)),
		ibcConnectionOpenTry: must(meter.Int64Counter(
			"ibc_connection_open_try",
			metric.WithDescription("Total number of IBC connection open-try handshakes"),
			metric.WithUnit("{count}"),
		)),
		ibcConnectionOpenAck: must(meter.Int64Counter(
			"ibc_connection_open_ack",
			metric.WithDescription("Total number of IBC connection open-ack handshakes"),
			metric.WithUnit("{count}"),
		)),
		ibcConnectionOpenConfirm: must(meter.Int64Counter(
			"ibc_connection_open_confirm",
			metric.WithDescription("Total number of IBC connection open-confirm handshakes"),
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
