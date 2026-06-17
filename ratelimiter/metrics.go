package ratelimiter

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	registryMeter = otel.Meter("ratelimiter")

	registryMetrics = struct {
		rejectedCounter metric.Int64Counter
	}{
		rejectedCounter: must(registryMeter.Int64Counter(
			"rpc_rate_limit_rejected_total",
			metric.WithDescription("Total RPC requests rejected by the per-IP rate limiter"),
			metric.WithUnit("{request}"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
