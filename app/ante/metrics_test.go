package ante

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestGetAnteMetricsNoPanic(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	m := getAnteMetrics()
	if m == nil {
		t.Fatal("getAnteMetrics() returned nil")
	}
}

func TestAnteMetricsPendingNonceAllEvents(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	ctx := context.Background()
	m := getAnteMetrics()
	for _, event := range []string{"added", "expired", "rejected", "accepted"} {
		m.pendingNonce.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("event", event)))
	}
}
