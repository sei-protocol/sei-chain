package indexer

import (
	tmprometheus "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
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
	BlockEventsSeconds tmprometheus.HistogramVec

	// Latency for indexing transaction events.
	TxEventsSeconds tmprometheus.HistogramVec

	// Number of complete blocks indexed.
	BlocksIndexed tmprometheus.CounterIntVec

	// Number of transactions indexed.
	TransactionsIndexed tmprometheus.CounterIntVec
}
