package evmrpc

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func summaryCountAndSum(t *testing.T, obs prometheus.Observer) (count uint64, sum float64) {
	t.Helper()
	m, ok := obs.(prometheus.Metric)
	require.True(t, ok, "Observer should implement prometheus.Metric")
	var pb dto.Metric
	require.NoError(t, m.Write(&pb))
	s := pb.GetSummary()
	require.NotNil(t, s)
	return s.GetSampleCount(), s.GetSampleSum()
}

func TestRecordRPCMetricsNoPanic(t *testing.T) {
	t.Parallel()
	initRPCTelemetryMetrics()

	// Unique endpoint so parallel runs of this test (or other package tests) do not
	// share the same label set on the CounterVec / SummaryVec.
	endpoint := "eth_smoke_" + t.Name()

	req := rpcRequestCounter.WithLabelValues(endpoint, "http", "true")
	reqBefore := testutil.ToFloat64(req)

	lat := rpcLatencySummary.WithLabelValues(endpoint, "http")
	latCountBefore, latSumBefore := summaryCountAndSum(t, lat)

	wsBefore := testutil.ToFloat64(websocketConnectCounter)

	recordRPCRequest(endpoint, "http", true)
	recordRPCLatency(endpoint, "http", time.Now().Add(-2*time.Millisecond))
	recordWebsocketConnect()

	require.InDelta(t, 1.0, testutil.ToFloat64(req)-reqBefore, 0.001, "sei_rpc_request_counter increment")

	latCountAfter, latSumAfter := summaryCountAndSum(t, lat)
	require.Equal(t, uint64(1), latCountAfter-latCountBefore, "sei_rpc_request_latency_ms sample count")
	latDelta := latSumAfter - latSumBefore
	require.GreaterOrEqual(t, latDelta, 0.1, "latency sum should reflect ~2ms observation")
	require.LessOrEqual(t, latDelta, 10, "latency sum upper bound (scheduling jitter)")

	wsDelta := testutil.ToFloat64(websocketConnectCounter) - wsBefore
	// Global counter: other tests that open websocket connections may increment it in the same window.
	require.GreaterOrEqual(t, wsDelta, 1.0, "sei_websocket_connect should increase")
}
