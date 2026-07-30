package basic

import "github.com/prometheus/client_golang/prometheus"

const (
	MetricsNamespace = "tendermint"
	MetricsSubsystem = "basic"
)

//go:generate go run ../../../../scripts/metricsgen -struct=Metrics

// Metrics contains metrics exposed by this package.
type Metrics struct {
	// simple metric that tracks the height of the chain.
	Height *prometheus.GaugeVec
}
