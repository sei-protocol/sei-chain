package evmrpc

import (
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// RPC Prometheus metrics are registered on prometheus.DefaultRegisterer (same as
// the Cosmos armon Prometheus sink) so names and types match production dashboards:
// sei_rpc_request_counter, sei_rpc_request_latency_ms (Summary with quantile labels),
// sei_websocket_connect.
//
// The global OpenTelemetry Prometheus exporter applies a sei_chain_ namespace and
// counter suffix rules that do not match these legacy series, so we use the
// Prometheus client here instead of otel.Meter for this path.

var (
	rpcTelemetryOnce        sync.Once
	rpcRequestCounter       *prometheus.CounterVec
	rpcLatencySummary       *prometheus.SummaryVec
	websocketConnectCounter prometheus.Counter
)

func initRPCTelemetryMetrics() {
	rpcTelemetryOnce.Do(func() {
		rpcRequestCounter = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "sei_rpc_request_counter",
				Help: "RPC endpoint request throughput",
			},
			[]string{"endpoint", "connection", "success"},
		)
		// Match armon/go-metrics prometheus sink objectives for MeasureSince samples.
		rpcLatencySummary = prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Name:       "sei_rpc_request_latency_ms",
				Help:       "RPC request latency in milliseconds",
				Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
				MaxAge:     10 * time.Second,
			},
			[]string{"endpoint", "connection"},
		)
		websocketConnectCounter = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "sei_websocket_connect",
				Help: "Number of new websocket connects",
			},
		)
		prometheus.MustRegister(rpcRequestCounter, rpcLatencySummary, websocketConnectCounter)
	})
}

func recordRPCRequest(endpoint, connection string, success bool) {
	initRPCTelemetryMetrics()
	rpcRequestCounter.WithLabelValues(endpoint, connection, strconv.FormatBool(success)).Inc()
}

func recordRPCLatency(endpoint, connection string, start time.Time) {
	initRPCTelemetryMetrics()
	// Match armon go-metrics MeasureSince: timer value = elapsed / time.Millisecond.
	ms := float64(time.Since(start).Nanoseconds()) / float64(time.Millisecond)
	rpcLatencySummary.WithLabelValues(endpoint, connection).Observe(ms)
}

func recordWebsocketConnect() {
	initRPCTelemetryMetrics()
	websocketConnectCounter.Inc()
}
