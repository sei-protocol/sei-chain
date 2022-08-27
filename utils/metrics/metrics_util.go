package metrics

import (
	"time"

	metrics "github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
)

func MeasureSudoExecutionDuration(msg_type string) {
	metrics.MeasureSinceWithLabels(
		[]string{"sei", "sudo", "duration", "milliseconds"},
		time.Now(),
		[]metrics.Label{telemetry.NewLabel("type", msg_type)},
	)
}

func IncrementSudoFailCount(msg_type string) {
	telemetry.IncrCounterWithLabels(
		[]string{"sei", "sudo", "error", "count"},
		1,
		[]metrics.Label{telemetry.NewLabel("type", msg_type)},
	)
}
