package types

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("mint")

	mintMetrics = struct {
		coinsMinted metric.Int64Gauge
	}{
		coinsMinted: must(meter.Int64Gauge(
			"mint_coins_minted",
			metric.WithDescription("Amount of coins minted by denom"),
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
