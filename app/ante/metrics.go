package ante

import (
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

type anteMetrics struct {
	once sync.Once

	pendingNonce metric.Int64Counter
}

var appAnteMetrics anteMetrics

// InitAnteMetrics registers all OTel instruments for the ante package.
// Safe to call concurrently; instruments are registered exactly once.
func InitAnteMetrics() {
	appAnteMetrics.once.Do(func() {
		meter := otel.Meter("app_ante")
		var err error
		if appAnteMetrics.pendingNonce, err = meter.Int64Counter(
			"app_pending_nonce_total",
			metric.WithDescription("Pending nonce events by type (added, expired, rejected, accepted)"),
		); err != nil {
			panic("ante metrics: " + err.Error())
		}
	})
}
