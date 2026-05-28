package mempool

import (
	"math"
	"strconv"

	"github.com/go-kit/kit/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
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
	// due to other constraints, e.g. mempool is full and no lower priority
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
	// whether an error occurred during transaction priority determination.
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

	// CompactTotal counts invocations of the txStore compact path, labeled by the
	// triggering call site. reason=insert_overflow fires when Insert pushes total
	// past hardLimit; reason=update fires on every Update recompute pass;
	// reason=reap fires when Reap is called with remove=true. Rate of
	// reason=insert_overflow is the capacity-pressure signal.
	CompactTotal metrics.Counter `metrics_labels:"reason"`

	// CompactDurationSeconds observes wall-clock duration of compact() — which
	// re-sorts the mempool in priority order and rebuilds the byHash/byNonce
	// indices. Complexity is O(m log m) over the full mempool, so the upper
	// buckets must accommodate large mempools (100k entries → 1-3s typical,
	// 5-10s under GC pressure).
	CompactDurationSeconds metrics.Histogram `metrics_bucketsizes:"0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10"`

	// PromotionTotal counts pending-to-ready transitions. Incremented once per
	// nonce advance inside the inline promotion loop in txStore.insert (EVM
	// path). Cosmos txs are auto-ready on insert and are not counted.
	PromotionTotal metrics.Counter

	// Utilisation mirrors the same ratio the CheckTx drop gate evaluates:
	// total count / (cfg.Size + cfg.PendingSize). Exposed as a gauge so
	// recording rules don't need to re-derive it from Size+PendingSize.
	Utilisation metrics.Gauge
}

func (m *Metrics) observeCheckTxPriorityDistribution(priority int64, hint bool, senderNodeID types.NodeID, isError bool) {
	normalizedPriority := float64(priority) / float64(math.MaxInt64) // Normalize to [0.0, 1.0]
	m.CheckTxPriorityDistribution.With(
		"hint", strconv.FormatBool(hint),
		"local", strconv.FormatBool(senderNodeID == ""),
		"error", strconv.FormatBool(isError),
	).Observe(normalizedPriority)
}
