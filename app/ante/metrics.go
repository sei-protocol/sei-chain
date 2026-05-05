package ante

import (
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

type anteMetrics struct {
	pendingNonce metric.Int64Counter
}

// getAnteMetrics returns the package-level OTel instruments, initializing them
// lazily on first call (after the global MeterProvider is set in NewApp).
var getAnteMetrics = sync.OnceValue(func() *anteMetrics {
	m := &anteMetrics{}
	meter := otel.Meter("app_ante")
	var err error
	if m.pendingNonce, err = meter.Int64Counter(
		"app_pending_nonce_total",
		metric.WithDescription("Pending nonce events by type (added, expired, rejected, accepted)"),
	); err != nil {
		panic("ante anteMetrics: " + err.Error())
	}
	return m
})
