package evmrpc

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// JSON-RPC and websocket connection metrics use the OpenTelemetry Meter API.
// The process-wide MeterProvider (e.g. Prometheus exporter with namespace) is
// configured by the application.

var (
	rpcTelemetryMeter = otel.Meter("evmrpc_rpc")

	rpcTelemetryMetrics = struct {
		requests              metric.Int64Counter
		requestLatencyMs      metric.Float64Histogram
		websocketConnects     metric.Int64Counter
		filterLogFetchBatches metric.Int64Counter
	}{
		requests: must(rpcTelemetryMeter.Int64Counter(
			"evmrpc_rpc_requests_total",
			metric.WithDescription("RPC endpoint request throughput"),
			metric.WithUnit("{count}"),
		)),
		requestLatencyMs: must(rpcTelemetryMeter.Float64Histogram(
			"evmrpc_rpc_request_latency_ms",
			metric.WithDescription("RPC request latency in milliseconds (labeled by success)"),
			metric.WithUnit("ms"),
		)),
		websocketConnects: must(rpcTelemetryMeter.Int64Counter(
			"evmrpc_websocket_connects_total",
			metric.WithDescription("Number of new websocket connections"),
			metric.WithUnit("{count}"),
		)),
		filterLogFetchBatches: must(rpcTelemetryMeter.Int64Counter(
			"evmrpc_filter_log_fetch_batches_total",
			metric.WithDescription("Internal filter/getLogs block batches completed (per pipeline path, not per RPC)"),
			metric.WithUnit("{batch}"),
		)),
	}
)

func recordRPCRequest(ctx context.Context, endpoint, connection string, success bool) {
	rpcTelemetryMetrics.requests.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("endpoint", endpoint),
			attribute.String("connection", connection),
			attribute.Bool("success", success),
		),
	)
}

func recordRPCLatency(ctx context.Context, endpoint, connection string, success bool, start time.Time) {
	ms := float64(time.Since(start).Nanoseconds()) / float64(time.Millisecond)
	rpcTelemetryMetrics.requestLatencyMs.Record(ctx, ms,
		metric.WithAttributes(
			attribute.String("endpoint", endpoint),
			attribute.String("connection", connection),
			attribute.Bool("success", success),
		),
	)
}

func recordWebsocketConnect(ctx context.Context) {
	rpcTelemetryMetrics.websocketConnects.Add(ctx, 1)
}

func recordFilterLogFetchBatchComplete(ctx context.Context, pipeline string) {
	rpcTelemetryMetrics.filterLogFetchBatches.Add(ctx, 1,
		metric.WithAttributes(attribute.String("pipeline", pipeline)),
	)
}
