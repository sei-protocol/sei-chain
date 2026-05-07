package app

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-cosmos/version"
)

const appMeterName = "app"

// histogramBuckets aligns with block-time SLO thresholds
// (p50 ≤ 500ms, p95 ≤ 1.5s, p99 ≤ 2.5s) expressed in seconds; 3s/4s refine quantiles just above the p99 line.
var histogramBuckets = metric.WithExplicitBucketBoundaries(
	0.025, 0.05, 0.1, 0.25, 0.5, 0.75, 1.0, 1.5, 2.0, 2.5, 3.0, 4.0, 5.0, 10.0,
)

// millisecondBuckets is for metrics that typically complete in under 100ms, expressed in seconds.
// Covers µs-range fast paths (25µs–1ms) and occasional slower outliers up to 100ms.
var millisecondBuckets = metric.WithExplicitBucketBoundaries(
	0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1,
)

type metrics struct {
	mu          sync.Mutex
	initialized bool

	// ABCI phase durations
	beginBlockDuration     metric.Float64Histogram
	endBlockDuration       metric.Float64Histogram
	moduleEndBlockDuration metric.Float64Histogram
	checkTxDuration        metric.Float64Histogram
	deliverTxDuration      metric.Float64Histogram
	deliverBatchTxDuration metric.Float64Histogram

	// Commit duration
	commitDuration metric.Float64Histogram

	// Block processing duration by execution type
	blockProcessDuration metric.Float64Histogram

	// Per-tx counts and gas
	txCount       metric.Int64Counter
	txProcessType metric.Int64Counter
	txGas         metric.Int64Counter
	txGasUsed     metric.Int64Gauge
	txGasWanted   metric.Int64Gauge

	// App-level flow counters
	optimisticProcessing metric.Int64Counter
	failedGasWantedCheck metric.Int64Counter
	gigaFallback         metric.Int64Counter

	// Light invariance check
	invarianceDuration      metric.Float64Histogram
	invarianceInvalidKey    metric.Int64Counter
	invarianceUnmarshalFail metric.Int64Counter
}

// appMetrics is the package-level OTel instrument set, initialized once in NewApp.
var appMetrics metrics

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}

// initAppMetrics registers all OTel instruments for the app package.
// Safe to call concurrently; instruments are registered exactly once.
func initAppMetrics() {
	appMetrics.mu.Lock()
	defer appMetrics.mu.Unlock()
	if appMetrics.initialized {
		return
	}
	meter := otel.Meter(appMeterName)

	appMetrics.beginBlockDuration = must(meter.Float64Histogram(
		"app_abci_begin_block_duration_seconds",
		metric.WithDescription("Duration of ABCI BeginBlock"),
		metric.WithUnit("s"),
		millisecondBuckets,
	))

	appMetrics.endBlockDuration = must(meter.Float64Histogram(
		"app_abci_end_block_duration_seconds",
		metric.WithDescription("Duration of ABCI EndBlock"),
		metric.WithUnit("s"),
		millisecondBuckets,
	))

	appMetrics.moduleEndBlockDuration = must(meter.Float64Histogram(
		"app_abci_module_end_block_duration_seconds",
		metric.WithDescription("Duration of module EndBlock calls within ABCI EndBlock"),
		metric.WithUnit("s"),
		millisecondBuckets,
	))

	appMetrics.checkTxDuration = must(meter.Float64Histogram(
		"app_abci_check_tx_duration_seconds",
		metric.WithDescription("Duration of ABCI CheckTx"),
		metric.WithUnit("s"),
		millisecondBuckets,
	))

	appMetrics.deliverTxDuration = must(meter.Float64Histogram(
		"app_abci_deliver_tx_duration_seconds",
		metric.WithDescription("Duration of ABCI DeliverTx"),
		metric.WithUnit("s"),
		millisecondBuckets,
	))

	appMetrics.deliverBatchTxDuration = must(meter.Float64Histogram(
		"app_abci_deliver_batch_tx_duration_seconds",
		metric.WithDescription("Duration of ABCI DeliverTxBatch"),
		metric.WithUnit("s"),
		millisecondBuckets,
	))

	appMetrics.commitDuration = must(meter.Float64Histogram(
		"app_abci_commit_duration_seconds",
		metric.WithDescription("Duration of ABCI Commit (state write to disk)"),
		metric.WithUnit("s"),
		millisecondBuckets,
	))

	appMetrics.blockProcessDuration = must(meter.Float64Histogram(
		"app_block_process_duration_seconds",
		metric.WithDescription("Duration of block tx processing by execution type"),
		metric.WithUnit("s"),
		histogramBuckets,
	))

	appMetrics.txCount = must(meter.Int64Counter(
		"app_tx_count_total",
		metric.WithDescription("Number of transactions delivered"),
	))

	appMetrics.txProcessType = must(meter.Int64Counter(
		"app_tx_process_type_total",
		metric.WithDescription("Transactions processed by execution type"),
	))

	appMetrics.txGas = must(meter.Int64Counter(
		"app_tx_gas_total",
		metric.WithDescription("Cumulative transaction gas by type (gas_used, gas_wanted)"),
	))

	appMetrics.txGasUsed = must(meter.Int64Gauge(
		"app_tx_gas_used",
		metric.WithDescription("Gas used by the most recently delivered transaction"),
	))

	appMetrics.txGasWanted = must(meter.Int64Gauge(
		"app_tx_gas_wanted",
		metric.WithDescription("Gas wanted by the most recently delivered transaction"),
	))

	appMetrics.optimisticProcessing = must(meter.Int64Counter(
		"app_optimistic_processing_total",
		metric.WithDescription("Optimistic processing attempts; enabled=true means cache hit, false means miss"),
	))

	appMetrics.failedGasWantedCheck = must(meter.Int64Counter(
		"app_failed_total_gas_wanted_check_total",
		metric.WithDescription("Proposals rejected because total block gas wanted exceeded max"),
	))

	appMetrics.gigaFallback = must(meter.Int64Counter(
		"app_giga_fallback_to_v2_total",
		metric.WithDescription("Times giga executor fell back to V2 processing"),
	))

	appMetrics.invarianceDuration = must(meter.Float64Histogram(
		"app_lightinvariance_supply_duration_seconds",
		metric.WithDescription("Duration of light invariance total supply check"),
		metric.WithUnit("s"),
		millisecondBuckets,
	))

	appMetrics.invarianceInvalidKey = must(meter.Int64Counter(
		"app_lightinvariance_supply_invalid_key_total",
		metric.WithDescription("Invalid changed-pair keys detected during invariance check"),
	))

	appMetrics.invarianceUnmarshalFail = must(meter.Int64Counter(
		"app_lightinvariance_supply_unmarshal_failure_total",
		metric.WithDescription("Unmarshal failures during invariance supply check"),
	))

	// Build-info observable gauge replaces GaugeSeidVersionAndCommit (called per BeginBlock).
	// The callback fires on every Prometheus scrape; no per-block call site is needed.
	// TODO(PLT-327): remove metrics.GaugeSeidVersionAndCommit call in abci.go once app_build_info verified
	vi := version.NewInfo()
	_ = must(meter.Int64ObservableGauge(
		"app_build_info",
		metric.WithDescription("Running binary build info; value is always 1"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(1,
				metric.WithAttributes(
					attribute.String("seid_version", vi.Version),
					attribute.String("commit", vi.GitCommit),
				),
			)
			return nil
		}),
	))
	appMetrics.initialized = true
}
