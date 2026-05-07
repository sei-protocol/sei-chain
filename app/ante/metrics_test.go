package ante

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestInitAnteMetricsNoPanic(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	InitAnteMetrics()
}

func TestAnteMetricsPendingNonceAllEvents(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	InitAnteMetrics()
	ctx := context.Background()
	for _, event := range []string{"added", "expired", "rejected", "accepted"} {
		appAnteMetrics.pendingNonce.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("event", event)))
	}
}
