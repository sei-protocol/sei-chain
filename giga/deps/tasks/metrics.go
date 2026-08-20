package tasks

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// taskMetrics mirrors sei-cosmos/tasks/metrics.go's instruments of the same
// name/scope so the sei-cosmos and giga fork schedulers merge into single
// scheduler_retries/scheduler_incarnations series. Keep description/unit
// byte-identical across both declarations or the OTel SDK stops deduping.
var (
	meter = otel.Meter("seicosmos_tasks")

	taskMetrics = struct {
		retries      metric.Int64Counter
		incarnations metric.Int64Counter
	}{
		retries: must(meter.Int64Counter(
			"scheduler_retries",
			metric.WithDescription("Number of OCC scheduler transaction retries"),
			metric.WithUnit("{count}"),
		)),
		incarnations: must(meter.Int64Counter(
			"scheduler_incarnations",
			metric.WithDescription("Sum of per-round maximum incarnations in the OCC scheduler"),
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
