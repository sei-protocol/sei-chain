package metrics

import (
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
