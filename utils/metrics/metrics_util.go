package metrics

import (
	"time"

	metrics "github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/prometheus/client_golang/prometheus"
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
//  seid_version_and_commit
func GaugeSeidVersionAndCommit(version string, commit string) {
	opsQueued := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "seid_version_and_commit",
		},
		[]string{
			"seid_version",
			"commit",
		},
	)
	prometheus.MustRegister(opsQueued)
	opsQueued.With(prometheus.Labels{"seid_version": version, "commit": commit}).Add(1)
}
