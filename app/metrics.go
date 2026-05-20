package app

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-cosmos/version"
)

var (
	meter = otel.Meter("app")
	vi    = version.NewInfo()

	// fineGrainedBuckets (numbers are in seconds) aligns with block-processing
	// latency SLO thresholds (p50 ≤ 500ms, p95 ≤ 1.5s, p99 ≤ 2.5s) in seconds; 3s, 4s refine quantiles just above the p99 line.
	fineGrainedBuckets = metric.WithExplicitBucketBoundaries(
		0.025, 0.05, 0.1, 0.25, 0.5, 0.75, 1.0, 1.5, 2.0, 2.5, 3.0, 4.0, 5.0, 10.0,
	)

	// finerGrainedBuckets (numbers are in seconds) used for operations that are too fast for fineGrainedBuckets
	finerGrainedBuckets = metric.WithExplicitBucketBoundaries(
		0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10,
	)

	// blockGasWantedBuckets covers the 0–50 M gas range (current MaxGasWanted cap) with
	// 1 M steps up to 10 M and 5 M steps thereafter so both lightly- and heavily-loaded
	// blocks get useful quantiles.
	blockGasWantedBuckets = metric.WithExplicitBucketBoundaries(
		1e6, 2e6, 3e6, 4e6, 5e6, 6e6, 7e6, 8e6, 9e6, 10e6,
		15e6, 20e6, 25e6, 30e6, 35e6, 40e6, 45e6, 50e6,
	)

	// blockGasWantedRatioBuckets covers the 0.0–1.0 utilisation range so we can see
	// how close blocks are to the MaxGasWanted cap.
	blockGasWantedRatioBuckets = metric.WithExplicitBucketBoundaries(
		0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 1.0,
	)

	appMetrics = struct {
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

		// App-level flow counters
		optimisticProcessing metric.Int64Counter
		failedGasWantedCheck metric.Int64Counter
		gigaFallback         metric.Int64Counter

		// Per-block gas utilisation
		blockGasWanted      metric.Float64Histogram
		blockGasWantedRatio metric.Float64Histogram

		// Light invariance check
		invarianceDuration      metric.Float64Histogram
		invarianceInvalidKey    metric.Int64Counter
		invarianceUnmarshalFail metric.Int64Counter

		versionInfo metric.Int64ObservableGauge
	}{
		beginBlockDuration: must(meter.Float64Histogram(
			"app_abci_begin_block_duration",
			metric.WithDescription("Duration of ABCI BeginBlock"),
			metric.WithUnit("s"),
			finerGrainedBuckets,
		)),

		endBlockDuration: must(meter.Float64Histogram(
			"app_abci_end_block_duration",
			metric.WithDescription("Duration of ABCI EndBlock"),
			metric.WithUnit("s"),
			finerGrainedBuckets,
		)),

		moduleEndBlockDuration: must(meter.Float64Histogram(
			"app_abci_module_end_block_duration",
			metric.WithDescription("Duration of module EndBlock calls within ABCI EndBlock"),
			metric.WithUnit("s"),
			finerGrainedBuckets,
		)),

		checkTxDuration: must(meter.Float64Histogram(
			"app_abci_check_tx_duration",
			metric.WithDescription("Duration of ABCI CheckTx"),
			metric.WithUnit("s"),
			finerGrainedBuckets,
		)),

		deliverTxDuration: must(meter.Float64Histogram(
			"app_abci_deliver_tx_duration",
			metric.WithDescription("Duration of ABCI DeliverTx"),
			metric.WithUnit("s"),
			finerGrainedBuckets,
		)),

		deliverBatchTxDuration: must(meter.Float64Histogram(
			"app_abci_deliver_batch_tx_duration",
			metric.WithDescription("Duration of ABCI DeliverTxBatch"),
			metric.WithUnit("s"),
			finerGrainedBuckets,
		)),

		commitDuration: must(meter.Float64Histogram(
			"app_abci_commit_duration",
			metric.WithDescription("Duration of ABCI Commit (state write to disk)"),
			metric.WithUnit("s"),
			finerGrainedBuckets,
		)),

		blockProcessDuration: must(meter.Float64Histogram(
			"app_block_process_duration",
			metric.WithDescription("Duration of block tx processing by execution type"),
			metric.WithUnit("s"),
			fineGrainedBuckets,
		)),

		txCount: must(meter.Int64Counter(
			"app_tx_count",
			metric.WithDescription("Number of transactions delivered"),
			metric.WithUnit("{count}"),
		)),

		txProcessType: must(meter.Int64Counter(
			"app_tx_process_type",
			metric.WithDescription("Transactions processed by execution type"),
			metric.WithUnit("{count}"),
		)),

		txGas: must(meter.Int64Counter(
			"app_tx_gas",
			metric.WithDescription("Cumulative transaction gas by type (gas_used, gas_wanted)"),
			metric.WithUnit("{count}"),
		)),

		optimisticProcessing: must(meter.Int64Counter(
			"app_optimistic_processing",
			metric.WithDescription("Optimistic processing attempts; enabled:true means cache hit, false means miss"),
			metric.WithUnit("{count}"),
		)),

		failedGasWantedCheck: must(meter.Int64Counter(
			"app_failed_total_gas_wanted_check",
			metric.WithDescription("Proposals rejected because total block gas wanted exceeded max"),
			metric.WithUnit("{count}"),
		)),

		gigaFallback: must(meter.Int64Counter(
			"app_giga_fallback_to_v2",
			metric.WithDescription("Times giga executor fell back to V2 processing"),
			metric.WithUnit("{count}"),
		)),

		blockGasWanted: must(meter.Float64Histogram(
			"app_block_gas_wanted",
			metric.WithDescription("Per-block total gas wanted across all transactions"),
			metric.WithUnit("{gas}"),
			blockGasWantedBuckets,
		)),

		blockGasWantedRatio: must(meter.Float64Histogram(
			"app_block_gas_wanted_ratio",
			metric.WithDescription("Per-block ratio of total gas wanted to MaxGasWanted consensus parameter"),
			metric.WithUnit("1"),
			blockGasWantedRatioBuckets,
		)),

		invarianceDuration: must(meter.Float64Histogram(
			"app_lightinvariance_supply_duration",
			metric.WithDescription("Duration of light invariance total supply check"),
			metric.WithUnit("s"),
			finerGrainedBuckets,
		)),

		invarianceInvalidKey: must(meter.Int64Counter(
			"app_lightinvariance_supply_invalid_key",
			metric.WithDescription("Invalid changed-pair keys detected during invariance check"),
			metric.WithUnit("{count}"),
		)),

		invarianceUnmarshalFail: must(meter.Int64Counter(
			"app_lightinvariance_supply_unmarshal_failure",
			metric.WithDescription("Unmarshal failures during invariance supply check"),
			metric.WithUnit("{count}"),
		)),

		// The callback fires on every Prometheus scrape; no per-block call site is needed.
		// TODO(PLT-327): remove metrics.GaugeSeidVersionAndCommit call in abci.go once app_build_info verified
		versionInfo: must(meter.Int64ObservableGauge(
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
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
