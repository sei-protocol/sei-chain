package ante

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("evm_ante")

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
		)),

		nonceMismatch: must(meter.Int64Counter(
			"evm_nonce_mismatch",
			metric.WithDescription("EVM nonce mismatches by cause (too_high, too_low)"),
		)),

		effectiveGasPrice: must(meter.Float64Histogram(
			"evm_effective_gas_price",
			metric.WithDescription("Effective gas price for EVM transactions"),
		)),

		associationError: must(meter.Int64Counter(
			"evm_ante_association_error",
			metric.WithDescription("EVM address association errors by scenario and address type"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
