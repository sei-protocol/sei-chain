package eventlog

import tmprometheus "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"

const (
	// MetricsNamespace is the namespace shared by all Tendermint Prometheus metrics.
	MetricsNamespace = "tendermint"
	MetricsSubsystem = "eventlog"
)

//go:generate go run ../../scripts/metricsgen -struct=Metrics

// Metrics define the metrics exported by the eventlog package.
type Metrics struct {

	// Number of items currently resident in the event log.
	numItems tmprometheus.GaugeIntVec
}
