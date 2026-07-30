package statesync

import (
	"github.com/prometheus/client_golang/prometheus"
	tmprometheus "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const (
	// MetricsNamespace is the namespace shared by all Tendermint Prometheus metrics.
	MetricsNamespace = "tendermint"
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this package.
	MetricsSubsystem = "statesync"
)

//go:generate go run ../../scripts/metricsgen -struct=Metrics

// Metrics contains metrics exposed by this package.
type Metrics struct {
	// The total number of snapshots discovered.
	TotalSnapshots tmprometheus.CounterIntVec
	// The average processing time per chunk.
	ChunkProcessAvgTime *prometheus.GaugeVec
	// The height of the current snapshot the has been processed.
	SnapshotHeight tmprometheus.GaugeIntVec
	// The current number of chunks that have been processed.
	SnapshotChunk tmprometheus.CounterIntVec
	// The total number of chunks in the current snapshot.
	SnapshotChunkTotal tmprometheus.GaugeIntVec
	// The current number of blocks that have been back-filled.
	BackFilledBlocks tmprometheus.CounterIntVec
	// The total number of blocks that need to be back-filled.
	BackFillBlocksTotal tmprometheus.GaugeIntVec
}
