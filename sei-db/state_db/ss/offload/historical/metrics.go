package historical

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// fallbackMeterName is the OpenTelemetry meter for the node-side pruned-read
// fallback path, exported through the same process-wide MeterProvider.
const fallbackMeterName = "seidb_historical_fallback"

const (
	fallbackOutcomeCacheHit      = "cache_hit"
	fallbackOutcomeBackendHit    = "backend_hit"
	fallbackOutcomeBackendMiss   = "backend_miss"
	fallbackOutcomeBackendBehind = "backend_behind"
	fallbackOutcomeError         = "error"
)

// fallbackMetrics counts pruned point reads by operation (get/has) and outcome
// so operators can see how much load the read cache absorbs and how many reads
// actually reach the historical backend. Attributes are a closed set, so
// cardinality stays flat.
type fallbackMetrics struct {
	reads metric.Int64Counter
}

func newFallbackMetrics() *fallbackMetrics {
	meter := otel.Meter(fallbackMeterName)
	reads, _ := meter.Int64Counter(
		"historical_fallback_reads_total",
		metric.WithDescription("Pruned point reads routed to the historical fallback, by operation and outcome"),
		metric.WithUnit("{read}"),
	)
	return &fallbackMetrics{reads: reads}
}

func (m *fallbackMetrics) recordRead(op, outcome string) {
	if m == nil {
		return
	}
	m.reads.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("op", op),
		attribute.String("outcome", outcome),
	))
}
