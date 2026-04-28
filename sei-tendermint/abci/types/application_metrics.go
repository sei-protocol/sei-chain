package types

import "github.com/go-kit/kit/metrics"

const (
	// ProxyMetricsSubsystem is a subsystem shared by all proxy application metrics.
	ProxyMetricsSubsystem = "abci_connection"
)

// ProxyMetrics contains the prometheus metrics exposed by ProxyApplication.
type ProxyMetrics struct {
	// Timing for each ABCI method.
	MethodTiming metrics.Histogram
}
