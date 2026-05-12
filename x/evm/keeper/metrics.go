package keeper

import (
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var evmMillisecondBuckets = metric.WithExplicitBucketBoundaries(
	0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10,
)

type evmKeeperMetricsType struct {
	once sync.Once

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
}

var evmKeeperMetrics evmKeeperMetricsType

func mustMetric[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}

// InitEvmKeeperMetrics registers all OTel instruments for the x/evm keeper package.
// Safe to call concurrently; instruments are registered exactly once.
func InitEvmKeeperMetrics() {
	evmKeeperMetrics.once.Do(func() {
		meter := otel.Meter("evm_keeper")

		evmKeeperMetrics.beginBlockerDuration = mustMetric(meter.Float64Histogram(
			"evm_abci_begin_blocker_duration_seconds",
			metric.WithDescription("Duration of EVM module BeginBlock"),
			metric.WithUnit("s"),
			evmMillisecondBuckets,
		))

		evmKeeperMetrics.endBlockerDuration = mustMetric(meter.Float64Histogram(
			"evm_abci_end_blocker_duration_seconds",
			metric.WithDescription("Duration of EVM module EndBlock"),
			metric.WithUnit("s"),
			evmMillisecondBuckets,
		))

		evmKeeperMetrics.blockBaseFee = mustMetric(meter.Float64Gauge(
			"evm_block_base_fee",
			metric.WithDescription("Current EVM block base fee per gas"),
		))

		evmKeeperMetrics.panics = mustMetric(meter.Int64Counter(
			"evm_panics_total",
			metric.WithDescription("Number of panics recovered during EVM transaction processing"),
		))

		evmKeeperMetrics.errors = mustMetric(meter.Int64Counter(
			"evm_errors_total",
			metric.WithDescription("EVM processing errors by type (state_transition, stateDB_finalize, write_receipt, apply_message, vm_execution)"),
		))

		evmKeeperMetrics.receiptStatus = mustMetric(meter.Int64Counter(
			"evm_receipt_status_total",
			metric.WithDescription("EVM transaction receipt outcomes by status (success, failed)"),
		))

		evmKeeperMetrics.associationError = mustMetric(meter.Int64Counter(
			"evm_association_error_total",
			metric.WithDescription("EVM address association errors by scenario and address type"),
		))

		evmKeeperMetrics.zeroStorageProcessedKeys = mustMetric(meter.Int64Counter(
			"evm_zero_storage_processed_keys_total",
			metric.WithDescription("Storage slots scanned during zero-value cleanup"),
		))

		evmKeeperMetrics.zeroStoragePrunedKeys = mustMetric(meter.Int64Counter(
			"evm_zero_storage_pruned_keys_total",
			metric.WithDescription("Zero-value storage slots deleted during cleanup"),
		))

		evmKeeperMetrics.zeroStoragePrunedBytes = mustMetric(meter.Int64Counter(
			"evm_zero_storage_pruned_bytes_total",
			metric.WithDescription("Bytes reclaimed by zero-value storage slot cleanup"),
		))
	})
}
