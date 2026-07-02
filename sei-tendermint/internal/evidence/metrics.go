package evidence

import tmmetrics "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"

const (
	// MetricsNamespace is the namespace shared by all Tendermint Prometheus metrics.
	MetricsNamespace = "tendermint"
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this
	// package.
	MetricsSubsystem = "evidence_pool"
)

//go:generate go run ../../scripts/metricsgen -struct=Metrics

// Metrics contains metrics exposed by this package.
// see MetricsProvider for descriptions.
type Metrics struct {
	// Number of pending evidence in the evidence pool.
	NumEvidence *tmmetrics.GaugeIntVec
}
