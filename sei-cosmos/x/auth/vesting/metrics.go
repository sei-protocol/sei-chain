package vesting

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_x_auth_vesting")

	vestingMetrics = struct {
		newAccount    metric.Int64Counter
		accountAmount metric.Int64Gauge
	}{
		newAccount: must(meter.Int64Counter(
			"new_account",
			metric.WithDescription("Number of new vesting accounts created"),
			metric.WithUnit("{count}"),
		)),
		accountAmount: must(meter.Int64Gauge(
			"account_amount",
			metric.WithDescription("Amount funded into the last new vesting account by denomination"),
			metric.WithUnit("{utoken}"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
