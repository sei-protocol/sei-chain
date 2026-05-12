package ante

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestInitEvmAnteMetricsNoPanic(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	InitEvmAnteMetrics()
}

func TestEvmAnteMetricsAllInstrumentsUsable(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	InitEvmAnteMetrics()

	ctx := context.Background()

	for _, event := range []string{"added", "expired", "rejected", "accepted"} {
		evmAnteMetrics.pendingNonce.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("event", event)))
	}
	for _, cause := range []string{"too_high", "too_low"} {
		evmAnteMetrics.nonceMismatch.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("cause", cause)))
	}
	evmAnteMetrics.effectiveGasPrice.Record(ctx, 1e9)
	evmAnteMetrics.associationError.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("scenario", "associate_tx_insufficient_funds"), attribute.String("type", "sei")))
}
