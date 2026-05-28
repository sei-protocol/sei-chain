package tasks

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

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
			metric.WithDescription("Maximum incarnation seen in OCC scheduler round"),
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
