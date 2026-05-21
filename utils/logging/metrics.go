package logging

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("utils_logging")

	loggingMetrics = struct {
		logNotDoneAfter metric.Int64Counter
	}{
		logNotDoneAfter: must(meter.Int64Counter(
			"log_not_done_after",
			metric.WithDescription("Number of times an operation was still not finished after the expected duration by label"),
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
