package indexer

import (
	"github.com/go-kit/kit/metrics"
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
	BlockEventsSeconds metrics.Histogram

	// Latency for indexing transaction events.
	TxEventsSeconds metrics.Histogram

	// Number of complete blocks indexed.
	BlocksIndexed metrics.Counter

	// Number of transactions indexed.
	TransactionsIndexed metrics.Counter
}
