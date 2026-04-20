package evmrpc

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	endpointKey   = "endpoint"
	connectionKey = "connection"
	successKey    = "success"
)

// JSON-RPC and websocket connection metrics use the OpenTelemetry Meter API.
// The process-wide MeterProvider (e.g. Prometheus exporter with namespace) is
// configured by the application.

var (
	rpcTelemetryMeter = otel.Meter("evmrpc")

	metrics = struct {
		requestLatencySeconds metric.Float64Histogram
		wsConnectionCount     metric.Int64Counter
	}{
		requestLatencySeconds: must(rpcTelemetryMeter.Float64Histogram(
			"evmrpc_request_latency_seconds",
			metric.WithDescription("RPC request latency in seconds (labeled by success)"),
			metric.WithUnit("s"),
		)),
		wsConnectionCount: must(rpcTelemetryMeter.Int64Counter(
			"evmrpc_websocket_connects_total",
			metric.WithDescription("Number of new websocket connections"),
			metric.WithUnit("{count}"),
		)),
	}
)

func recordRPCLatency(ctx context.Context, endpoint, connection string, success bool, start time.Time) {
	seconds := time.Since(start).Seconds()
	metrics.requestLatencySeconds.Record(ctx, seconds,
		metric.WithAttributes(
			attribute.String(endpointKey, endpoint),
			attribute.String(connectionKey, connection),
			attribute.Bool(successKey, success),
		),
	)
}

func recordWebsocketConnect(ctx context.Context) {
	metrics.wsConnectionCount.Add(ctx, 1)
}
