package types

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_store_types")

	storeMetrics = struct {
		gasExceeded  metric.Int64Counter
		boundedCache metric.Int64Gauge
	}{
		gasExceeded: must(meter.Int64Counter(
			"gas_exceeded",
			metric.WithDescription("Number of gas exceeded errors by error type and descriptor"),
			metric.WithUnit("{count}"),
		)),
		boundedCache: must(meter.Int64Gauge(
			"bounded_cache",
			metric.WithDescription("Number of keys evicted in the last bounded cache eviction by type"),
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
