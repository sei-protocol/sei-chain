package wasmbinding

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("wasmbinding")

	wasmQueryMetrics = struct {
		associationError metric.Int64Counter
		sdkError         metric.Int64Counter
	}{
		associationError: must(meter.Int64Counter(
			"wasm_query_association_error",
			metric.WithDescription("Association errors during wasm query handling by scenario and address type"),
			metric.WithUnit("{count}"),
		)),
		sdkError: must(meter.Int64Counter(
			"wasm_query_sdk_error",
			metric.WithDescription("SDK errors during wasm query handling by scenario, codespace, and code"),
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
