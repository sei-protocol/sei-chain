package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ibc_core_client_keeper")

	ibcClientMetrics = struct {
		ibcClientCreate       metric.Int64Counter
		ibcClientUpdate       metric.Int64Counter
		ibcClientUpgrade      metric.Int64Counter
		ibcClientMisbehaviour metric.Int64Counter
	}{
		ibcClientCreate: must(meter.Int64Counter(
			"ibc_client_create",
			metric.WithDescription("Total number of IBC client creates"),
			metric.WithUnit("{count}"),
		)),
		ibcClientUpdate: must(meter.Int64Counter(
			"ibc_client_update",
			metric.WithDescription("Total number of IBC client updates"),
			metric.WithUnit("{count}"),
		)),
		ibcClientUpgrade: must(meter.Int64Counter(
			"ibc_client_upgrade",
			metric.WithDescription("Total number of IBC client upgrades"),
			metric.WithUnit("{count}"),
		)),
		ibcClientMisbehaviour: must(meter.Int64Counter(
			"ibc_client_misbehaviour",
			metric.WithDescription("Total number of IBC client misbehaviour events"),
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
