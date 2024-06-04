package metrics

import (
	"math/big"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	metrics "github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
)

// Measures the time taken to execute a sudo msg
// Metric Names:
//
//	sei_sudo_duration_miliseconds
//	sei_sudo_duration_miliseconds_count
//	sei_sudo_duration_miliseconds_sum
func MeasureSudoExecutionDuration(start time.Time, msgType string) {
	metrics.MeasureSinceWithLabels(
		[]string{"sei", "sudo", "duration", "milliseconds"},
		start.UTC(),
		[]metrics.Label{telemetry.NewLabel("type", msgType)},
	)
}

// Measures failed sudo execution count
// Metric Name:
//
//	sei_sudo_error_count
func IncrementSudoFailCount(msgType string) {
	telemetry.IncrCounterWithLabels(
		[]string{"sei", "sudo", "error", "count"},
		1,
		[]metrics.Label{telemetry.NewLabel("type", msgType)},
	)
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
	metrics.IncrCounterWithLabels(
		[]string{"sei", "tx", "process", "type"},
		1,
		[]metrics.Label{telemetry.NewLabel("type", processType)},
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
//	sei_tx_process_type_count
func IncrDagBuildErrorCounter(reason string) {
	metrics.IncrCounterWithLabels(
		[]string{"sei", "dag", "build", "error"},
		1,
		[]metrics.Label{telemetry.NewLabel("reason", reason)},
	)
}

// Counts the number of concurrent transactions that failed
// Metric Names:
//
//	sei_tx_concurrent_delivertx_error
func IncrFailedConcurrentDeliverTxCounter() {
	metrics.IncrCounterWithLabels(
		[]string{"sei", "tx", "concurrent", "delievertx", "error"},
		1,
		[]metrics.Label{},
	)
}

// Counts the number of operations that failed due to operation timeout
// Metric Names:
//
//	sei_log_not_done_after_counter
func IncrLogIfNotDoneAfter(label string) {
	metrics.IncrCounterWithLabels(
		[]string{"sei", "log", "not", "done", "after"},
		1,
		[]metrics.Label{
			telemetry.NewLabel("label", label),
		},
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

// Measures throughput
// Metric Name:
//
//	sei_throughput_<metric_name>
func SetThroughputMetric(metricName string, value float32) {
	telemetry.SetGauge(
		value,
		"sei", "throughput", metricName,
	)
}

// Measures number of new websocket connects
// Metric Name:
//
//	sei_websocket_connect
func IncWebsocketConnects() {
	telemetry.IncrCounterWithLabels(
		[]string{"sei", "websocket", "connect"},
		1,
		nil,
	)
}

// Measures number of times a denom's price is updated
// Metric Name:
//
//	sei_oracle_price_update_count
func IncrPriceUpdateDenom(denom string) {
	telemetry.IncrCounterWithLabels(
		[]string{"sei", "oracle", "price", "update"},
		1,
		[]metrics.Label{telemetry.NewLabel("denom", denom)},
	)
}

// Measures throughput per message type
// Metric Name:
//
//	sei_throughput_<metric_name>
func SetThroughputMetricByType(metricName string, value float32, msgType string) {
	telemetry.SetGaugeWithLabels(
		[]string{"sei", "loadtest", "tps", metricName},
		value,
		[]metrics.Label{telemetry.NewLabel("msg_type", msgType)},
	)
}

// Measures the number of times the total block gas wanted in the proposal exceeds the max
// Metric Name:
//
//	sei_failed_total_gas_wanted_check
func IncrFailedTotalGasWantedCheck(proposer string) {
	telemetry.IncrCounterWithLabels(
		[]string{"sei", "failed", "total", "gas", "wanted", "check"},
		1,
		[]metrics.Label{telemetry.NewLabel("proposer", proposer)},
	)
}

// Measures the number of times the total block gas wanted in the proposal exceeds the max
// Metric Name:
//
//	sei_failed_total_gas_wanted_check
func IncrValidatorSlashed(proposer string) {
	telemetry.IncrCounterWithLabels(
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
	telemetry.IncrCounterWithLabels(
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
	telemetry.IncrCounterWithLabels(
		[]string{"sei", "optimistic", "processing", "counter"},
		float32(1),
		[]metrics.Label{telemetry.NewLabel("enabled", strconv.FormatBool(enabled))},
	)
}

// Measures RPC endpoint request throughput
// Metric Name:
//
//	sei_rpc_request_counter
func IncrementRpcRequestCounter(endpoint string, connectionType string, success bool) {
	telemetry.IncrCounterWithLabels(
		[]string{"sei", "rpc", "request", "counter"},
		float32(1),
		[]metrics.Label{
			telemetry.NewLabel("endpoint", endpoint),
			telemetry.NewLabel("connection", connectionType),
			telemetry.NewLabel("success", strconv.FormatBool(success)),
		},
	)
}

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

// IncrProducerEventCount increments the counter for events produced.
// This metric counts the number of events produced by the system.
// Metric Name:
//
//	sei_loadtest_produce_count
func IncrProducerEventCount(msgType string) {
	telemetry.IncrCounterWithLabels(
		[]string{"sei", "loadtest", "produce", "count"},
		1,
		[]metrics.Label{telemetry.NewLabel("msg_type", msgType)},
	)
}

// IncrConsumerEventCount increments the counter for events consumed.
// This metric counts the number of events consumed by the system.
// Metric Name:
//
//	sei_loadtest_consume_count
func IncrConsumerEventCount(msgType string) {
	telemetry.IncrCounterWithLabels(
		[]string{"sei", "loadtest", "consume", "count"},
		1,
		[]metrics.Label{telemetry.NewLabel("msg_type", msgType)},
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
