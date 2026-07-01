package metrics

import (
	"errors"
	"fmt"
	"math/big"
	"runtime/debug"
	"strconv"
	"time"

	metrics "github.com/armon/go-metrics"
	"github.com/prometheus/otlptranslator"
	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/resource"
	sdk "go.opentelemetry.io/otel/sdk/metric"
)

func SetupOtelMetricsProvider(chainID string) error {
	if chainID == "" {
		return fmt.Errorf("chainID must not be empty")
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			"",
			attribute.String("chain_id", chainID),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create OTel resource: %w", err)
	}

	metricsExporter, err := prometheus.New(
		prometheus.WithNamespace("sei_chain"),
		prometheus.WithTranslationStrategy(otlptranslator.UnderscoreEscapingWithSuffixes),
		prometheus.WithResourceAsConstantLabels(
			attribute.NewAllowKeysFilter("chain_id"),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create Prometheus exporter: %w", err)
	}
	otel.SetMeterProvider(sdk.NewMeterProvider(
		sdk.WithResource(res),
		sdk.WithReader(metricsExporter),
	))
	return nil
}

func SafeTelemetryIncrCounter(val float32, keys ...string) {
	defer func() {
		if e := recover(); e != nil {
			debug.PrintStack()
			return
		}
	}()
	telemetry.IncrCounter(val, keys...)
}

func SafeTelemetryIncrCounterWithLabels(keys []string, val float32, labels []metrics.Label) {
	defer func() {
		if e := recover(); e != nil {
			debug.PrintStack()
			return
		}
	}()
	telemetry.IncrCounterWithLabels(keys, val, labels)
}

func SafeMetricsIncrCounterWithLabels(keys []string, val float32, labels []metrics.Label) {
	defer func() {
		if e := recover(); e != nil {
			debug.PrintStack()
			return
		}
	}()
	metrics.IncrCounterWithLabels(keys, val, labels)
}

// Gauge metric with seid version and git commit as labels
// Metric Name:
//
//	seid_version_and_commit
func GaugeSeidVersionAndCommit(version string, commit string) {
	telemetry.SetGaugeWithLabels(
		[]string{"seid_version_and_commit"},
		1,
		[]metrics.Label{telemetry.NewLabel("seid_version", version), telemetry.NewLabel("commit", commit)},
	)
}

// sei_tx_process_type_count
func IncrTxProcessTypeCounter(processType string) {
	SafeMetricsIncrCounterWithLabels(
		[]string{"sei", "tx", "process", "type"},
		1,
		[]metrics.Label{telemetry.NewLabel("type", processType)},
	)
}

// sei_giga_fallback_to_v2_count
func IncrGigaFallbackToV2Counter() {
	SafeMetricsIncrCounterWithLabels(
		[]string{"sei", "giga", "fallback", "to", "v2", "count"},
		1,
		[]metrics.Label{},
	)
}

// Measures the time taken to process a block by the process type
// Metric Names:
//
//	sei_process_block_miliseconds
//	sei_process_block_miliseconds_count
//	sei_process_block_miliseconds_sum
func BlockProcessLatency(start time.Time, processType string) {
	metrics.MeasureSinceWithLabels(
		[]string{"sei", "process", "block", "milliseconds"},
		start.UTC(),
		[]metrics.Label{telemetry.NewLabel("type", processType)},
	)
}

// Measures the time taken to execute a sudo msg
// Metric Names:
//
//	sei_deliver_tx_duration_miliseconds
//	sei_deliver_tx_duration_miliseconds_count
//	sei_deliver_tx_duration_miliseconds_sum
func MeasureDeliverTxDuration(start time.Time) {
	metrics.MeasureSince(
		[]string{"sei", "deliver", "tx", "milliseconds"},
		start.UTC(),
	)
}

// Measures the time taken to execute a batch tx
// Metric Names:
//
//	sei_deliver_batch_tx_duration_miliseconds
//	sei_deliver_batch_tx_duration_miliseconds_count
//	sei_deliver_batch_tx_duration_miliseconds_sum
func MeasureDeliverBatchTxDuration(start time.Time) {
	metrics.MeasureSince(
		[]string{"sei", "deliver", "batch", "tx", "milliseconds"},
		start.UTC(),
	)
}

// sei_oracle_vote_penalty_count
func SetOracleVotePenaltyCount(count uint64, valAddr string, penaltyType string) {
	metrics.SetGaugeWithLabels(
		[]string{"sei", "oracle", "vote", "penalty", "count"},
		float32(count),
		[]metrics.Label{
			telemetry.NewLabel("type", penaltyType),
			telemetry.NewLabel("validator", valAddr),
		},
	)
}

// sei_epoch_new
func SetEpochNew(epochNum uint64) {
	metrics.SetGauge(
		[]string{"sei", "epoch", "new"},
		float32(epochNum),
	)
}

// sei_evm_zero_storage_pruned_keys
func IncrEvmZeroStoragePrunedKeys(count uint64) {
	SafeTelemetryIncrCounter(
		float32(count),
		"sei", "evm", "zero", "storage", "pruned", "keys",
	)
}

// sei_evm_zero_storage_processed_keys
func IncrEvmZeroStorageProcessedKeys(count uint64) {
	SafeTelemetryIncrCounter(
		float32(count),
		"sei", "evm", "zero", "storage", "processed", "keys",
	)
}

// sei_evm_zero_storage_pruned_bytes
func IncrEvmZeroStoragePrunedBytes(bytes uint64) {
	SafeTelemetryIncrCounter(
		float32(bytes),
		"sei", "evm", "zero", "storage", "pruned", "bytes",
	)
}

// Measures number of times a denom's price is updated
// Metric Name:
//
//	sei_oracle_price_update_count
func IncrPriceUpdateDenom(denom string) {
	SafeTelemetryIncrCounterWithLabels(
		[]string{"sei", "oracle", "price", "update"},
		1,
		[]metrics.Label{telemetry.NewLabel("denom", denom)},
	)
}

// Measures the number of times the total block gas wanted in the proposal exceeds the max
// Metric Name:
//
//	sei_failed_total_gas_wanted_check
func IncrFailedTotalGasWantedCheck(proposer string) {
	SafeTelemetryIncrCounterWithLabels(
		[]string{"sei", "failed", "total", "gas", "wanted", "check"},
		1,
		[]metrics.Label{telemetry.NewLabel("proposer", proposer)},
	)
}

// Measures number of times a denom's price is updated
// Metric Name:
//
//	sei_oracle_price_update_count
func SetCoinsMinted(amount uint64, denom string) {
	telemetry.SetGaugeWithLabels(
		[]string{"sei", "mint", "coins"},
		float32(amount),
		[]metrics.Label{telemetry.NewLabel("denom", denom)},
	)
}

// Measures the number of times the total block gas wanted in the proposal exceeds the max
// Metric Name:
//
//	sei_tx_gas_counter
func IncrGasCounter(gasType string, value int64) {
	const maxSafeFloat32 = 1<<24 - 1 // Maximum safe integer representable in float32 (16,777,215)

	if value < 0 {
		// Log negative values but don't panic - this shouldn't happen in normal operation
		telemetry.IncrCounterWithLabels(
			[]string{"sei", "tx", "gas", "counter", "error"},
			1,
			[]metrics.Label{
				telemetry.NewLabel("type", gasType),
				telemetry.NewLabel("error", "negative_value"),
			},
		)
		return
	}

	if value > maxSafeFloat32 {
		// Cap the value to prevent overflow while logging the incident
		telemetry.IncrCounterWithLabels(
			[]string{"sei", "tx", "gas", "counter", "overflow"},
			1,
			[]metrics.Label{
				telemetry.NewLabel("type", gasType),
				telemetry.NewLabel("original_value", "capped"),
			},
		)
		value = maxSafeFloat32
	}

	SafeTelemetryIncrCounterWithLabels(
		[]string{"sei", "tx", "gas", "counter"},
		float32(value),
		[]metrics.Label{telemetry.NewLabel("type", gasType)},
	)
}

// Measures the number of times optimistic processing runs
// Metric Name:
//
//	sei_optimistic_processing_counter
func IncrementOptimisticProcessingCounter(enabled bool) {
	SafeTelemetryIncrCounterWithLabels(
		[]string{"sei", "optimistic", "processing", "counter"},
		float32(1),
		[]metrics.Label{telemetry.NewLabel("enabled", strconv.FormatBool(enabled))},
	)
}

// TODO(PLT-326): remove once dashboards are migrated to evmrpc_* OTEL metrics.
// Measures number of new websocket connects
// Metric Name:
//
//	sei_websocket_connect
func IncWebsocketConnects() {
	SafeTelemetryIncrCounterWithLabels([]string{"sei", "websocket", "connect"}, 1, nil)
}

// TODO(PLT-326): remove once dashboards are migrated to evmrpc_* OTEL metrics.
// Measures RPC endpoint request throughput
// Metric Name:
//
//	sei_rpc_request_counter
func IncrementRpcRequestCounter(endpoint string, connectionType string, success bool) {
	SafeTelemetryIncrCounterWithLabels(
		[]string{"sei", "rpc", "request", "counter"},
		float32(1),
		[]metrics.Label{
			telemetry.NewLabel("endpoint", endpoint),
			telemetry.NewLabel("connection", connectionType),
			telemetry.NewLabel("success", strconv.FormatBool(success)),
		},
	)
}

// TODO(PLT-326): remove once dashboards are migrated to evmrpc_* OTEL metrics.
// Measures the RPC request latency in milliseconds
// Metric Name:
//
//	sei_rpc_request_latency_ms
func MeasureRpcRequestLatency(endpoint string, connectionType string, startTime time.Time) {
	metrics.MeasureSinceWithLabels(
		[]string{"sei", "rpc", "request", "latency_ms"},
		startTime.UTC(),
		[]metrics.Label{
			telemetry.NewLabel("endpoint", endpoint),
			telemetry.NewLabel("connection", connectionType),
		},
	)
}

func IncrementErrorMetrics(scenario string, err error) {
	if err == nil {
		return
	}
	var assocErr types.AssociationMissingErr
	if errors.As(err, &assocErr) {
		IncrementAssociationError(scenario, assocErr)
		return
	}
	// add other error types to handle as metrics
}

func IncrementAssociationError(scenario string, err types.AssociationMissingErr) {
	SafeTelemetryIncrCounterWithLabels(
		[]string{"sei", "association", "error"},
		1,
		[]metrics.Label{
			telemetry.NewLabel("scenario", scenario),
			telemetry.NewLabel("type", err.AddressType()),
		},
	)
}

func IncrementNonceMismatch(tooHigh bool) {
	cause := "too_low"
	if tooHigh {
		cause = "too_high"
	}
	SafeTelemetryIncrCounterWithLabels(
		[]string{"sei", "nonce", "mismatch"},
		1,
		[]metrics.Label{
			telemetry.NewLabel("cause", cause),
		},
	)
}

func AddHistogramMetric(key []string, value float32) {
	metrics.AddSample(key, value)
}

// Gauge for gas price paid for transactions
// Metric Name:
//
// sei_evm_effective_gas_price
func HistogramEvmEffectiveGasPrice(gasPrice *big.Int) {
	AddHistogramMetric(
		[]string{"sei", "evm", "effective", "gas", "price"},
		float32(gasPrice.Uint64()),
	)
}

// Gauge for block base fee
// Metric Name:
//
// sei_evm_block_base_fee
func GaugeEvmBlockBaseFee(baseFee *big.Int, blockHeight int64) {
	metrics.SetGauge(
		[]string{"sei", "evm", "block", "base", "fee"},
		float32(baseFee.Uint64()),
	)
}
