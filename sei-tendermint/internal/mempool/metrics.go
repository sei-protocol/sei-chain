package mempool

import (
	"math"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// MetricsNamespace is the namespace shared by all Tendermint Prometheus metrics.
	MetricsNamespace = "tendermint"
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this
	// package.
	MetricsSubsystem = "mempool"
)

var (
	mempoolMeter = otel.Meter("tendermint_mempool")

	otelMetrics = struct {
		compactTotal           metric.Int64Counter
		compactDurationSeconds metric.Float64Histogram
	}{
		compactTotal: utils.OrPanic1(mempoolMeter.Int64Counter(
			"tendermint_mempool_compact_total",
			metric.WithDescription("Number of compact() invocations, labeled by call site (insert_overflow, update, reap)."),
		)),
		compactDurationSeconds: utils.OrPanic1(mempoolMeter.Float64Histogram(
			"tendermint_mempool_compact_duration_seconds",
			metric.WithDescription("Wall-clock duration of compact(), which re-sorts and rebuilds indices over the full mempool (O(m log m))."),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(stdprometheus.ExponentialBucketsRange(0.001, 30, 14)...),
		)),
	}

	triggerInsertOverflowAttr = metric.WithAttributes(attribute.String("trigger", "insert_overflow"))
	triggerUpdateAttr         = metric.WithAttributes(attribute.String("trigger", "update"))
	triggerReapAttr           = metric.WithAttributes(attribute.String("trigger", "reap"))
)

//go:generate go run ../../scripts/metricsgen -struct=Metrics

// Metrics contains metrics exposed by this package.
// see MetricsProvider for descriptions.
type Metrics struct {
	// Number of uncommitted transactions in the mempool.
	Size *prometheus.GaugeVec

	// Number of pending transactions in mempool
	PendingSize *prometheus.GaugeVec

	// Number of cached transactions in the mempool cache.
	CacheSize *prometheus.GaugeVec

	// Accumulated transaction sizes in bytes.
	TxSizeBytes *prometheus.CounterVec

	// Total current mempool uncommitted txs bytes
	TotalTxsSizeBytes *prometheus.GaugeVec

	// Track max number of occurrences for a duplicate tx
	DuplicateTxMaxOccurrences *prometheus.GaugeVec

	// Track the total number of occurrences for all duplicate txs
	DuplicateTxTotalOccurrences *prometheus.GaugeVec

	// Track the number of unique duplicate transactions
	NumberOfDuplicateTxs *prometheus.GaugeVec

	// Track the number of unique new tx transactions
	NumberOfNonDuplicateTxs *prometheus.GaugeVec

	// Track the number of checkTx calls
	NumberOfSuccessfulCheckTxs *prometheus.CounterVec

	// Track the number of failed checkTx calls
	NumberOfFailedCheckTxs *prometheus.CounterVec

	// Track the number of checkTx from local removed tx
	NumberOfLocalCheckTx *prometheus.CounterVec

	// Number of failed transactions.
	FailedTxs *prometheus.CounterVec

	// RejectedTxs defines the number of rejected transactions. These are
	// transactions that passed CheckTx but failed to make it into the mempool
	// due to other constraints, e.g. mempool is full and no lower priority
	// transactions exist in the mempool.
	//metrics:Number of rejected transactions.
	RejectedTxs *prometheus.CounterVec

	// EvictedTxs defines the number of evicted transactions. These are valid
	// transactions that passed CheckTx and existed in the mempool but were later
	// evicted to make room for higher priority valid transactions that passed
	// CheckTx.
	//metrics:Number of evicted transactions.
	EvictedTxs *prometheus.CounterVec

	// ExpiredTxs defines the number of expired transactions. These are valid
	// transactions that passed CheckTx and existed in the mempool but were not
	// get picked up in time and eventually got expired and removed from mempool
	//metrics:Number of expired transactions.
	ExpiredTxs *prometheus.CounterVec

	// Number of times transactions are rechecked in the mempool.
	RecheckTimes *prometheus.CounterVec

	// Number of removed tx from mempool
	RemovedTxs *prometheus.CounterVec

	// Number of txs inserted to mempool
	InsertedTxs *prometheus.CounterVec

	// CheckTxPriorityDistribution is a histogram of the priority of transactions
	// submitted via CheckTx, labeled by whether a priority hint was provided,
	// whether the transaction was submitted locally (i.e. no sender node ID), and
	// whether an error occurred during transaction priority determination.
	//
	// Note that the priority is normalized as a float64 value between zero and
	// maximum tx priority.
	CheckTxPriorityDistribution *prometheus.HistogramVec `metrics_buckettype:"exprange" metrics_bucketsizes:"0.000001, 1.0, 20" metrics_labels:"hint, local, error"`

	// CheckTxDroppedByPriorityHint is the number of transactions that were dropped
	// due to low priority based on the priority hint.
	CheckTxDroppedByPriorityHint *prometheus.CounterVec

	// CheckTxMetDropUtilisationThreshold is the number of transactions for which CheckTx was executed while the mempool
	// utilisation was above the configured threshold. Note that not all such transactions are dropped, only those that also have a low priority.
	CheckTxMetDropUtilisationThreshold *prometheus.CounterVec
}

func (m *Metrics) observeCheckTxPriorityDistribution(priority int64, hint bool, senderNodeID types.NodeID, isError bool) {
	normalizedPriority := float64(priority) / float64(math.MaxInt64) // Normalize to [0.0, 1.0]
	m.CheckTxPriorityDistributionAt(
		strconv.FormatBool(hint),
		strconv.FormatBool(senderNodeID == ""),
		strconv.FormatBool(isError),
	).Observe(normalizedPriority)
}
