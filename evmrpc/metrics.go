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
	endpointKey    = "endpoint"
	connectionKey  = "connection"
	successKey     = "success"
	errorClassKey  = "error_class"
	jsonrpcCodeKey = "jsonrpc_code"
	// error_class values; empty string ("") means success.
	errorClassPanic              = "panic"
	errorClassExecutionReverted  = "execution_reverted"
	errorClassEVMNotSupported    = "evm_not_supported"
	errorClassSeiLegacyDisabled  = "sei_legacy_disabled"
	errorClassAssociationMissing = "association_missing"
	errorClassJSONRPCError       = "jsonrpc_error"
	errorClassUnknown            = "unknown"
	// jsonrpc_code bucket values; empty string ("") means no code (success or untyped error).
	// Ranges follow JSON-RPC 2.0: predefined codes -32700..-32600, server codes -32099..-32000.
	jsonrpcCodeBucketSpec   = "spec"
	jsonrpcCodeBucketServer = "server"
	jsonrpcCodeBucketOther  = "other"
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
			metric.WithDescription("RPC request latency in seconds (labeled by success, error_class, and jsonrpc_code bucket)"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(
				0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25,
				0.5, 1, 2.5, 5, 10, 30,
			),
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

func classifyRPCMetricError(err error, panicked bool) (errorClass string, jsonrpcCodeBucket string) {
	if panicked {
		return errorClassPanic, bucketJSONRPCCode(internalErrorCode)
	}
	if err == nil {
		return "", "" // success: omit both to keep the high-volume happy path cheap
	}
	var rev *RevertError
	if errors.As(err, &rev) {
		return errorClassExecutionReverted, bucketJSONRPCCode(int64(rev.ErrorCode()))
	}
	var notSup *ErrEVMNotSupported
	if errors.As(err, &notSup) {
		return errorClassEVMNotSupported, bucketJSONRPCCode(int64(notSup.ErrorCode()))
	}
	var legacy *errSeiLegacyNotEnabled
	if errors.As(err, &legacy) {
		return errorClassSeiLegacyDisabled, bucketJSONRPCCode(int64(legacy.ErrorCode()))
	}
	var assoc types.AssociationMissingErr
	if errors.As(err, &assoc) {
		return errorClassAssociationMissing, ""
	}
	var coder rpcJSONCoder
	if errors.As(err, &coder) {
		return errorClassJSONRPCError, bucketJSONRPCCode(int64(coder.ErrorCode()))
	}
	return errorClassUnknown, ""
}

// bucketJSONRPCCode maps a raw JSON-RPC error code to a low-cardinality string bucket.
// JSON-RPC 2.0 predefined range: -32700..-32600; server-defined range: -32099..-32000.
func bucketJSONRPCCode(code int64) string {
	switch {
	case code >= -32700 && code <= -32600:
		return jsonrpcCodeBucketSpec
	case code >= -32099 && code <= -32000:
		return jsonrpcCodeBucketServer
	default:
		return jsonrpcCodeBucketOther
	}
}

func recordRPCLatency(ctx context.Context, endpoint, connection string, success bool, err error, panicked bool, start time.Time) {
	seconds := time.Since(start).Seconds()
	errorClass, jsonrpcCodeBucket := classifyRPCMetricError(err, panicked)
	metrics.requestLatencySeconds.Record(ctx, seconds,
		metric.WithAttributes(
			attribute.String(endpointKey, endpoint),
			attribute.String(connectionKey, connection),
			attribute.Bool(successKey, success),
			attribute.String(errorClassKey, errorClass),
			attribute.String(jsonrpcCodeKey, jsonrpcCodeBucket),
		),
	)
}

func recordWebsocketConnect(ctx context.Context) {
	metrics.wsConnectionCount.Add(ctx, 1)
}
