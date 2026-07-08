package commented

import "github.com/prometheus/client_golang/prometheus"

const (
	MetricsNamespace = "tendermint"
	MetricsSubsystem = "commented"
)

//go:generate go run ../../../../scripts/metricsgen -struct=Metrics

type Metrics struct {
	// Height of the chain.
	// We expect multi-line comments to parse correctly.
	Field *prometheus.GaugeVec
}
