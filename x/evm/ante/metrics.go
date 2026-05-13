package ante

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("evm_ante")

	addedEventAttribute    = metric.WithAttributes(attribute.String("event", "added"))
	expiredEventAttribute  = metric.WithAttributes(attribute.String("event", "expired"))
	rejectedEventAttribute = metric.WithAttributes(attribute.String("event", "rejected"))
	acceptedEventAttribute = metric.WithAttributes(attribute.String("event", "accepted"))

	// evmEffectiveGasPriceBucketBoundaries are explicit histogram upper bounds in wei per gas
	// (EIP-1559 effective gas price). Spaced from 1 wei through 100k gwei for useful quantiles.
	evmEffectiveGasPriceBucketBoundaries = []float64{
		1, 10, 100, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8,
		5e8, 1e9, 2e9, 5e9, 1e10, 2e10, 5e10,
		1e11, 2e11, 5e11, 1e12, 5e12, 1e13, 5e13, 1e14,
	}

	evmAnteMetrics = struct {
		pendingNonce  metric.Int64Counter
		nonceMismatch metric.Int64Counter

		// Gas price histogram
		effectiveGasPrice metric.Float64Histogram

		// Association errors
		associationError metric.Int64Counter
	}{
		pendingNonce: must(meter.Int64Counter(
			"evm_pending_nonce",
			metric.WithDescription("EVM pending nonce events by type (added, expired, rejected, accepted)"),
			metric.WithUnit("{count}"),
		)),

		nonceMismatch: must(meter.Int64Counter(
			"evm_nonce_mismatch",
			metric.WithDescription("EVM nonce mismatches by cause (too_high, too_low)"),
			metric.WithUnit("{count}"),
		)),

		effectiveGasPrice: must(meter.Float64Histogram(
			"evm_effective_gas_price",
			metric.WithDescription("Effective gas price (wei per gas) for EVM transactions"),
			metric.WithUnit("{wei}/{gas}"),
			metric.WithExplicitBucketBoundaries(evmEffectiveGasPriceBucketBoundaries...),
		)),

		associationError: must(meter.Int64Counter(
			"evm_ante_association_error",
			metric.WithDescription("EVM address association errors by scenario and address type"),
			metric.WithUnit("{count}"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
