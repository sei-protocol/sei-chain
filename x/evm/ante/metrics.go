package ante

import (
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

type evmAnteMetricsType struct {
	once sync.Once

	// Nonce tracking
	pendingNonce  metric.Int64Counter
	nonceMismatch metric.Int64Counter

	// Gas price histogram
	effectiveGasPrice metric.Float64Histogram

	// Association errors
	associationError metric.Int64Counter
}

var evmAnteMetrics evmAnteMetricsType

func mustAnteMetric[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}

// InitEvmAnteMetrics registers all OTel instruments for the x/evm ante package.
// Safe to call concurrently; instruments are registered exactly once.
func InitEvmAnteMetrics() {
	evmAnteMetrics.once.Do(func() {
		meter := otel.Meter("evm_ante")

		evmAnteMetrics.pendingNonce = mustAnteMetric(meter.Int64Counter(
			"evm_pending_nonce_total",
			metric.WithDescription("EVM pending nonce events by type (added, expired, rejected, accepted)"),
		))

		evmAnteMetrics.nonceMismatch = mustAnteMetric(meter.Int64Counter(
			"evm_nonce_mismatch_total",
			metric.WithDescription("EVM nonce mismatches by cause (too_high, too_low)"),
		))

		evmAnteMetrics.effectiveGasPrice = mustAnteMetric(meter.Float64Histogram(
			"evm_effective_gas_price",
			metric.WithDescription("Effective gas price for EVM transactions"),
		))

		evmAnteMetrics.associationError = mustAnteMetric(meter.Int64Counter(
			"evm_ante_association_error_total",
			metric.WithDescription("EVM address association errors by scenario and address type"),
		))
	})
}
