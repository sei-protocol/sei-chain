package proxy

import tmprometheus "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"

const (
	// MetricsNamespace is the namespace shared by all Tendermint Prometheus metrics.
	MetricsNamespace = "tendermint"
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this package.
	MetricsSubsystem = "abci_connection"
)

//go:generate go run ../../scripts/metricsgen -struct=Metrics

// Metrics contains the prometheus metrics exposed by Proxy.
type Metrics struct {
	// Timing for each ABCI method.
	MethodTiming tmprometheus.HistogramVec `metrics_buckets:".0001,.0004,.002,.009,.02,.1,.65,2,6,25" metrics_labels:"method, type"`
}
