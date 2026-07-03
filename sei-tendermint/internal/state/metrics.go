package state

import (
	"github.com/prometheus/client_golang/prometheus"
	tmmetrics "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const (
	// MetricsNamespace is the namespace shared by all Tendermint Prometheus metrics.
	MetricsNamespace = "tendermint"
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this
	// package.
	MetricsSubsystem = "state"
)

//go:generate go run ../../scripts/metricsgen -struct=Metrics

// Metrics contains metrics exposed by this package.
type Metrics struct {
	// Time between BeginBlock and EndBlock.
	BlockProcessingTime *prometheus.HistogramVec `metrics_buckets:"exprange(0.01, 10, 10)"`

	// ConsensusParamUpdates is the total number of times the application has
	// udated the consensus params since process start.
	//metrics:Number of consensus parameter updates returned by the application since process start.
	ConsensusParamUpdates *tmmetrics.CounterIntVec

	// ValidatorSetUpdates is the total number of times the application has
	// udated the validator set since process start.
	//metrics:Number of validator set updates returned by the application since process start.
	ValidatorSetUpdates *tmmetrics.CounterIntVec

	// ApplicationCommitTime measures how long it takes to commit application state
	ApplicationCommitTime *prometheus.HistogramVec

	// UpdateMempoolTime measures how long it takes to update mempool after committing, including
	// reCheckTx
	UpdateMempoolTime *prometheus.HistogramVec

	// FinalizeBlockLatency measures how long it takes to run abci FinalizeBlock
	FinalizeBlockLatency *prometheus.HistogramVec `metrics_buckets:"exprange(0.01, 10, 10)"`

	// SaveBlockResponseLatency measures how long it takes to run save the FinalizeBlockRes
	SaveBlockResponseLatency *prometheus.HistogramVec `metrics_buckets:"exprange(0.01, 10, 10)"`

	// SaveBlockLatency measure how long it takes to save the block
	SaveBlockLatency *prometheus.HistogramVec `metrics_buckets:"exprange(0.01, 10, 10)"`

	// PruneBlockLatency measures how long it takes to prune block from blockstore
	PruneBlockLatency *prometheus.HistogramVec `metrics_buckets:"exprange(0.01, 10, 10)"`

	// FireEventsLatency measures how long it takes to fire events for indexing
	FireEventsLatency *prometheus.HistogramVec `metrics_buckets:"exprange(0.01, 10, 10)"`

	// ProposerPriorityHash encodes the first 6 bytes of the hash of the
	// current validator set's proposer priorities as a float64 value.
	// Exported periodically (every proposerPriorityHashInterval heights) for
	// operator visibility; divergence between validators at the same
	// ProposerPriorityHashHeight indicates corrupted ProposerPriority state.
	// Paired with ProposerPriorityHashHeight so operators can correlate.
	ProposerPriorityHash *prometheus.GaugeVec

	// ProposerPriorityHashHeight is the block height at which the most recent
	// ProposerPriorityHash was computed. Operators comparing hashes across
	// validators should only compare samples at the same height.
	ProposerPriorityHashHeight *tmmetrics.GaugeIntVec
}
