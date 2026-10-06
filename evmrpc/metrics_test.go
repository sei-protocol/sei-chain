package evmrpc

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMapWSAdmissionRejectReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{rpc.WSAdmissionReasonOversizeFrame, rejectReasonOversize},
		{rpc.WSAdmissionReasonBudgetWaitTimeout, rejectReasonBusy},
		{rpc.WSAdmissionReasonFrameAdmissionTimeout, rejectReasonBusy},
		{"future_reason", "future_reason"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := mapWSAdmissionRejectReason(tc.in); got != tc.want {
				t.Fatalf("mapWSAdmissionRejectReason(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRecordRPCMetricsNoPanic(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	endpoint := "eth_smoke_" + t.Name()
	recordRPCLatency(ctx, endpoint, "http", true, nil, false, time.Now().Add(-2*time.Millisecond))
	recordWebsocketConnect(ctx)
	recordRedirectedRequest(ctx, endpoint, "http")
}

func TestWSConnectionHandlerRecordsActiveConnections(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	previousActiveConnectionCount := metrics.wsActiveConnectionCount
	metrics.wsActiveConnectionCount = must(provider.Meter("evmrpc").Int64UpDownCounter(
		"evmrpc_websocket_connections",
	))
	t.Cleanup(func() {
		metrics.wsActiveConnectionCount = previousActiveConnectionCount
		require.NoError(t, provider.Shutdown(t.Context()))
	})

	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	handler := NewWSConnectionHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	}))
	go func() {
		defer close(finished)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()

	<-started
	require.Equal(t, int64(1), collectActiveWSConnections(t, reader))

	close(release)
	<-finished
	require.Equal(t, int64(0), collectActiveWSConnections(t, reader))
}

func collectActiveWSConnections(t *testing.T, reader *metric.ManualReader) int64 {
	t.Helper()

	var resourceMetrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &resourceMetrics))
	for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "evmrpc_websocket_connections" {
				continue
			}
			sum := m.Data.(metricdata.Sum[int64])
			require.Len(t, sum.DataPoints, 1)
			return sum.DataPoints[0].Value
		}
	}
	t.Fatal("evmrpc_websocket_connections metric not found")
	return 0
}

func TestClassifyRPCMetricError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		err       error
		panicked  bool
		wantClass string
		wantCode  string
	}{
		{name: "ok", err: nil, panicked: false, wantClass: "", wantCode: ""},
		{name: "panic", err: nil, panicked: true, wantClass: errorClassPanic, wantCode: jsonrpcCodeBucketSpec},
		{name: "panic_with_err", err: errors.New("ignored when panicked"), panicked: true, wantClass: errorClassPanic, wantCode: jsonrpcCodeBucketSpec},
		{name: "revert", err: NewRevertErrorFromError(errors.New("execution reverted")), wantClass: errorClassExecutionReverted, wantCode: jsonrpcCodeBucketOther},
		{name: "evm_not_supported", err: &ErrEVMNotSupported{Msg: "nope"}, wantClass: errorClassEVMNotSupported, wantCode: jsonrpcCodeBucketServer},
		{name: "sei_legacy", err: errSeiLegacyNotEnabledForTest("m"), wantClass: errorClassSeiLegacyDisabled, wantCode: jsonrpcCodeBucketSpec},
		{name: "association", err: types.NewAssociationMissingErr("0xabc"), wantClass: errorClassAssociationMissing, wantCode: ""},
		{name: "wrapped_revert", err: errors.Join(errors.New("outer"), NewRevertErrorFromError(errors.New("execution reverted"))), wantClass: errorClassExecutionReverted, wantCode: jsonrpcCodeBucketOther},
		{name: "unknown", err: errors.New("something else"), wantClass: errorClassUnknown, wantCode: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotClass, gotCode := classifyRPCMetricError(tc.err, tc.panicked)
			if gotClass != tc.wantClass || gotCode != tc.wantCode {
				t.Fatalf("classifyRPCMetricError() = (%q, %q), want (%q, %q)", gotClass, gotCode, tc.wantClass, tc.wantCode)
			}
		})
	}
}

// NewRevertErrorFromError builds a *RevertError for tests (minimal valid instance).
func NewRevertErrorFromError(err error) *RevertError {
	return &RevertError{error: err, reason: "0x"}
}

func errSeiLegacyNotEnabledForTest(method string) error {
	return &errSeiLegacyNotEnabled{method: method}
}
