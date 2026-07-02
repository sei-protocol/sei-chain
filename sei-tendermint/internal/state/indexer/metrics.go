package indexer

import (
	"github.com/prometheus/client_golang/prometheus"
)

//go:generate go run ../../../scripts/metricsgen -struct=Metrics

const (
	// MetricsNamespace is the namespace shared by all Tendermint Prometheus metrics.
	MetricsNamespace = "tendermint"
	// MetricsSubsystem is a the subsystem label for the indexer package.
	MetricsSubsystem = "indexer"
)

// Metrics contains metrics exposed by this package.
type Metrics struct {
	// Latency for indexing block events.
	BlockEventsSeconds *prometheus.HistogramVec

	// Latency for indexing transaction events.
	TxEventsSeconds *prometheus.HistogramVec

	// Number of complete blocks indexed.
	BlocksIndexed *prometheus.CounterVec

	// Number of transactions indexed.
	TransactionsIndexed *prometheus.CounterVec
}
