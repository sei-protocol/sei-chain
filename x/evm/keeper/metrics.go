package keeper

import (
	"math"
	"math/big"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	//numbers are in seconds
	finerGrainedBuckets = metric.WithExplicitBucketBoundaries(
		0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10,
	)
	meter = otel.Meter("evm_keeper")

	evmKeeperMetrics = struct {
		// ABCI phase durations
		beginBlockerDuration metric.Float64Histogram
		endBlockerDuration   metric.Float64Histogram

		// Block base fee (set each EndBlock)
		blockBaseFee metric.Float64Gauge

		// EVMTransaction error counters
		panics        metric.Int64Counter
		errors        metric.Int64Counter
		receiptStatus metric.Int64Counter

		// Association errors
		associationError metric.Int64Counter

		// Zero-storage cleanup counters
		zeroStorageProcessedKeys metric.Int64Counter
		zeroStoragePrunedKeys    metric.Int64Counter
		zeroStoragePrunedBytes   metric.Int64Counter
	}{
		beginBlockerDuration: must(meter.Float64Histogram(
			"evm_abci_begin_blocker_duration",
			metric.WithDescription("Duration of EVM module BeginBlock"),
			metric.WithUnit("s"),
			finerGrainedBuckets,
		)),

		endBlockerDuration: must(meter.Float64Histogram(
			"evm_abci_end_blocker_duration",
			metric.WithDescription("Duration of EVM module EndBlock"),
			metric.WithUnit("s"),
			finerGrainedBuckets,
		)),

		blockBaseFee: must(meter.Float64Gauge(
			"evm_block_base_fee",
			metric.WithDescription("Current EVM block base fee per gas"),
		)),

		panics: must(meter.Int64Counter(
			"evm_panics",
			metric.WithDescription("Number of panics recovered during EVM transaction processing"),
			metric.WithUnit("{count}"),
		)),

		errors: must(meter.Int64Counter(
			"evm_errors",
			metric.WithDescription("EVM processing errors by type (state_transition, stateDB_finalize, write_receipt, apply_message, vm_execution)"),
			metric.WithUnit("{count}"),
		)),

		receiptStatus: must(meter.Int64Counter(
			"evm_receipt_status",
			metric.WithDescription("EVM transaction receipt outcomes by status (success, failed)"),
			metric.WithUnit("{count}"),
		)),

		associationError: must(meter.Int64Counter(
			"evm_keeper_association_error",
			metric.WithDescription("EVM address association errors by scenario and address type"),
			metric.WithUnit("{count}"),
		)),

		zeroStorageProcessedKeys: must(meter.Int64Counter(
			"evm_zero_storage_processed_keys",
			metric.WithDescription("Storage slots scanned during zero-value cleanup"),
			metric.WithUnit("{count}"),
		)),

		zeroStoragePrunedKeys: must(meter.Int64Counter(
			"evm_zero_storage_pruned_keys",
			metric.WithDescription("Zero-value storage slots deleted during cleanup"),
			metric.WithUnit("{count}"),
		)),

		zeroStoragePrunedBytes: must(meter.Int64Counter(
			"evm_zero_storage_pruned_bytes",
			metric.WithDescription("Bytes reclaimed by zero-value storage slot cleanup"),
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

// bigIntToFloat64 converts a *big.Int to float64 without calling Uint64(), which
// has undefined behavior for values > math.MaxUint64 per the math/big docs.
func bigIntToFloat64(v *big.Int) float64 {
	if v == nil || v.Sign() < 0 {
		return 0
	}
	if v.IsUint64() {
		return float64(v.Uint64())
	}
	f, _ := new(big.Float).SetInt(v).Float64()
	if math.IsInf(f, 1) {
		return math.MaxFloat64
	}
	return f
}
