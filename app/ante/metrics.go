package ante

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var meter = otel.Meter("app_ante")
var anteMetrics = struct {
	pendingNonce metric.Int64Counter
}{
	pendingNonce: must(
		meter.Int64Counter(
			"app_pending_nonce",
			metric.WithDescription("Pending nonce events by type (added, expired, rejected, accepted)"),
			metric.WithUnit("{count}"))),
}

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
