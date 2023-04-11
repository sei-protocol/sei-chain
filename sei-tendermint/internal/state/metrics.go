package state

import (
	"github.com/go-kit/kit/metrics"
)

const (
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this
	// package.
	MetricsSubsystem = "state"
)

//go:generate go run ../../scripts/metricsgen -struct=Metrics

// Metrics contains metrics exposed by this package.
type Metrics struct {
	// Time between BeginBlock and EndBlock.
	BlockProcessingTime metrics.Histogram `metrics_buckettype:"exprange" metrics_bucketsizes:"0.01, 10, 10"`

	// ConsensusParamUpdates is the total number of times the application has
	// udated the consensus params since process start.
	//metrics:Number of consensus parameter updates returned by the application since process start.
	ConsensusParamUpdates metrics.Counter

	// ValidatorSetUpdates is the total number of times the application has
	// udated the validator set since process start.
	//metrics:Number of validator set updates returned by the application since process start.
	ValidatorSetUpdates metrics.Counter

	// ValidatorSetUpdates measures how long it takes async ABCI requests to be flushed before
	// committing application state
	FlushAppConnectionTime metrics.Histogram

	// ApplicationCommitTime meaures how long it takes to commit application state
	ApplicationCommitTime metrics.Histogram

	// UpdateMempoolTime meaures how long it takes to update mempool after commiting, including
	// reCheckTx
	UpdateMempoolTime metrics.Histogram
}
