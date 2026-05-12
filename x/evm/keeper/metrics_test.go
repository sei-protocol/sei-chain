package keeper

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestInitEvmKeeperMetricsNoPanic(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	InitEvmKeeperMetrics()
}

func TestEvmKeeperMetricsAllInstrumentsUsable(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	InitEvmKeeperMetrics()

	ctx := context.Background()

	evmKeeperMetrics.beginBlockerDuration.Record(ctx, 0.1)
	evmKeeperMetrics.endBlockerDuration.Record(ctx, 0.2)
	evmKeeperMetrics.blockBaseFee.Record(ctx, 1e9)

	evmKeeperMetrics.panics.Add(ctx, 1)
	evmKeeperMetrics.errors.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("type", "state_transition")))
	evmKeeperMetrics.errors.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("type", "stateDB_finalize")))
	evmKeeperMetrics.errors.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("type", "write_receipt")))
	evmKeeperMetrics.errors.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("type", "apply_message")))
	evmKeeperMetrics.errors.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("type", "vm_execution")))
	evmKeeperMetrics.receiptStatus.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("status", "success")))
	evmKeeperMetrics.receiptStatus.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("status", "failed")))
	evmKeeperMetrics.associationError.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("scenario", "evm_handle_internal_evm_delegate_call"), attribute.String("type", "sei")))

	evmKeeperMetrics.zeroStorageProcessedKeys.Add(ctx, 10)
	evmKeeperMetrics.zeroStoragePrunedKeys.Add(ctx, 5)
	evmKeeperMetrics.zeroStoragePrunedBytes.Add(ctx, 100)
}
