package mempool

import (
	"math"
	"strconv"

	"github.com/go-kit/kit/metrics"
	"github.com/tendermint/tendermint/types"
)

const (
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this
	// package.
	MetricsSubsystem = "mempool"
)

//go:generate go run ../../scripts/metricsgen -struct=Metrics

// Metrics contains metrics exposed by this package.
// see MetricsProvider for descriptions.
type Metrics struct {
	// Number of uncommitted transactions in the mempool.
	Size metrics.Gauge

	// Number of pending transactions in mempool
	PendingSize metrics.Gauge

	// Number of cached transactions in the mempool cache.
	CacheSize metrics.Gauge

	// Accumulated transaction sizes in bytes.
	TxSizeBytes metrics.Counter

	// Total current mempool uncommitted txs bytes
	TotalTxsSizeBytes metrics.Gauge

	// Track max number of occurrences for a duplicate tx
	DuplicateTxMaxOccurrences metrics.Gauge

	// Track the total number of occurrences for all duplicate txs
	DuplicateTxTotalOccurrences metrics.Gauge

	// Track the number of unique duplicate transactions
	NumberOfDuplicateTxs metrics.Gauge

	// Track the number of unique new tx transactions
	NumberOfNonDuplicateTxs metrics.Gauge

	// Track the number of checkTx calls
	NumberOfSuccessfulCheckTxs metrics.Counter

	// Track the number of failed checkTx calls
	NumberOfFailedCheckTxs metrics.Counter

	// Track the number of checkTx from local removed tx
	NumberOfLocalCheckTx metrics.Counter

	// Number of failed transactions.
	FailedTxs metrics.Counter

	// RejectedTxs defines the number of rejected transactions. These are
	// transactions that passed CheckTx but failed to make it into the mempool
	// due to resource limits, e.g. mempool is full and no lower priority
	// transactions exist in the mempool.
	//metrics:Number of rejected transactions.
	RejectedTxs metrics.Counter

	// EvictedTxs defines the number of evicted transactions. These are valid
	// transactions that passed CheckTx and existed in the mempool but were later
	// evicted to make room for higher priority valid transactions that passed
	// CheckTx.
	//metrics:Number of evicted transactions.
	EvictedTxs metrics.Counter

	// ExpiredTxs defines the number of expired transactions. These are valid
	// transactions that passed CheckTx and existed in the mempool but were not
	// get picked up in time and eventually got expired and removed from mempool
	//metrics:Number of expired transactions.
	ExpiredTxs metrics.Counter

	// Number of times transactions are rechecked in the mempool.
	RecheckTimes metrics.Counter

	// Number of removed tx from mempool
	RemovedTxs metrics.Counter

	// Number of txs inserted to mempool
	InsertedTxs metrics.Counter

	// CheckTxPriorityDistribution is a histogram of the priority of transactions
	// submitted via CheckTx, labeled by whether a priority hint was provided,
	// whether the transaction was submitted locally (i.e. no sender node ID), and
	// whether an error occured during transaction priority determination.
	//
	// Note that the priority is normalized as a float64 value between zero and
	// maximum tx priority.
	CheckTxPriorityDistribution metrics.Histogram `metrics_buckettype:"exprange" metrics_bucketsizes:"0.000001, 1.0, 20" metrics_labels:"hint, local, error"`

	// CheckTxDroppedByPriorityHint is the number of transactions that were dropped
	// due to low priority based on the priority hint.
	CheckTxDroppedByPriorityHint metrics.Counter

	// CheckTxMetDropUtilisationThreshold is the number of transactions for which CheckTx was executed while the mempool
	// utilisation was above the configured threshold. Note that not all such transactions are dropped, only those that also have a low priority.
	CheckTxMetDropUtilisationThreshold metrics.Counter
}

func (m *Metrics) observeCheckTxPriorityDistribution(priority int64, hint bool, senderNodeID types.NodeID, err error) {
	normalizedPriority := float64(priority) / float64(math.MaxInt64) // Normalize to [0.0, 1.0]
	m.CheckTxPriorityDistribution.With(
		"hint", strconv.FormatBool(hint),
		"local", strconv.FormatBool(senderNodeID == ""),
		"error", strconv.FormatBool(err != nil),
	).Observe(normalizedPriority)
}
