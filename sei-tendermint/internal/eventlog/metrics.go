package eventlog

import "github.com/go-kit/kit/metrics"

const (
	// MetricsNamespace is the namespace shared by all Tendermint Prometheus metrics.
	MetricsNamespace = "tendermint"
	MetricsSubsystem = "eventlog"
)

//go:generate go run ../../scripts/metricsgen -struct=Metrics

// Metrics define the metrics exported by the eventlog package.
type Metrics struct {

	// Number of items currently resident in the event log.
	numItems metrics.Gauge
}
