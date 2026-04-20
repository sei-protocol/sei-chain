package evmrpc

import (
	"context"
	"errors"
	"time"

	"github.com/sei-protocol/sei-chain/x/evm/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	endpointKey     = "endpoint"
	connectionKey   = "connection"
	successKey      = "success"
	errorClassKey   = "error_class"
	jsonrpcCodeKey  = "jsonrpc_code"
	errorClassOK    = "ok"
	errorClassPanic = "panic"
	// Well-known failure classes (low-cardinality; do not use raw error strings).
	errorClassExecutionReverted  = "execution_reverted"
	errorClassEVMNotSupported    = "evm_not_supported"
	errorClassSeiLegacyDisabled  = "sei_legacy_disabled"
	errorClassAssociationMissing = "association_missing"
	errorClassJSONRPCError       = "jsonrpc_error"
	errorClassUnknown            = "unknown"
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
			metric.WithDescription("RPC request latency in seconds (labeled by success, bounded error_class, and jsonrpc_code when known)"),
			metric.WithUnit("s"),
		)),
		wsConnectionCount: must(rpcTelemetryMeter.Int64Counter(
			"evmrpc_websocket_connects_total",
			metric.WithDescription("Number of new websocket connections"),
			metric.WithUnit("{count}"),
		)),
	}
)

// rpcJSONCoder matches errors serialized by go-ethereum rpc with a JSON-RPC error code.
type rpcJSONCoder interface {
	ErrorCode() int
}

func classifyRPCMetricError(err error, panicked bool) (errorClass string, jsonrpcCode int64) {
	if panicked {
		return errorClassPanic, internalErrorCode // JSON-RPC internal error
	}
	if err == nil {
		return errorClassOK, 0
	}
	var rev *RevertError
	if errors.As(err, &rev) {
		return errorClassExecutionReverted, int64(rev.ErrorCode())
	}
	var notSup *ErrEVMNotSupported
	if errors.As(err, &notSup) {
		return errorClassEVMNotSupported, int64(notSup.ErrorCode())
	}
	var legacy *errSeiLegacyNotEnabled
	if errors.As(err, &legacy) {
		return errorClassSeiLegacyDisabled, int64(legacy.ErrorCode())
	}
	var assoc types.AssociationMissingErr
	if errors.As(err, &assoc) {
		return errorClassAssociationMissing, 0
	}
	var coder rpcJSONCoder
	if errors.As(err, &coder) {
		return errorClassJSONRPCError, int64(coder.ErrorCode())
	}
	return errorClassUnknown, 0
}

func recordRPCLatency(ctx context.Context, endpoint, connection string, success bool, err error, panicked bool, start time.Time) {
	seconds := time.Since(start).Seconds()
	errorClass, jsonrpcCode := classifyRPCMetricError(err, panicked)
	metrics.requestLatencySeconds.Record(ctx, seconds,
		metric.WithAttributes(
			attribute.String(endpointKey, endpoint),
			attribute.String(connectionKey, connection),
			attribute.Bool(successKey, success),
			attribute.String(errorClassKey, errorClass),
			attribute.Int64(jsonrpcCodeKey, jsonrpcCode),
		),
	)
}

func recordWebsocketConnect(ctx context.Context) {
	metrics.wsConnectionCount.Add(ctx, 1)
}
