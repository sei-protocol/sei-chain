package metrics

import (
	"strconv"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const MetricsNamespace = "tendermint"
const MetricsSubsystem = "internal_autobahn_avail"

//go:generate go run github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen -struct=metrics
type metrics struct {
	// Road index of the highest observed commitQC.
	commitRoadIndex prometheus.GaugeIntVec

	// First global block number not covered by the highest observed CommitQC.
	commitGlobalBlockNumber prometheus.GaugeIntVec

	// Latency from proposal being constructed to commit being observed.
	proposalToCommitLatency prometheus.HistogramVec `metrics_buckets:"exp(0.01, 1.2, 35)"`
	// Latency between consecutive commits being observed.
	commitToCommitLatency prometheus.HistogramVec `metrics_labels:"timeouts" metrics_buckets:"none"`
	// Transactions included in locally produced lane blocks.
	producedTxs prometheus.CounterIntVec
	// Time a WaitForCapacity call spent blocked waiting for lane window.
	laneCapacityWaitLatency prometheus.HistogramVec `metrics_buckets:"exp(0.001, 2, 22)"`
	// Time a WaitForLaneQCs call spent blocked waiting for a new LaneQC.
	laneQcWaitLatency prometheus.HistogramVec `metrics_buckets:"exp(0.001, 2, 22)"`
	// Number of WaitForCapacity / WaitForLaneQCs calls currently blocked.
	inFlight prometheus.GaugeIntVec `metrics_labels:"wait"`
	// LaneQCs formed by lane votes reaching quorum.
	laneQcs prometheus.CounterIntVec
	// Lane votes newly stored.
	laneVotesIngested prometheus.CounterIntVec
}

type observed[T any] struct {
	time time.Time
	val  T
}

func newObserved[T any]() utils.Mutex[*utils.Option[observed[T]]] {
	return utils.NewMutex(utils.Alloc(utils.None[observed[T]]()))
}

var observedCommitQC = newObserved[*types.CommitQC]()

// ObserveCommitQC observes the CommitQC latency.
func ObserveCommitQC(qc *types.CommitQC) {
	now := time.Now()
	for mLast := range observedCommitQC.Lock() {
		if last, ok := mLast.Get(); ok {
			if last.val.Index() >= qc.Index() {
				return
			}
			// "timeouts" label is capped
			timeouts := "inf"
			if n := qc.Proposal().View().Number; n < 20 {
				timeouts = strconv.FormatUint(uint64(n), 10)
			}
			Global.commitToCommitLatencyAt(timeouts).Observe(now.Sub(last.time).Seconds())
		}
		Global.proposalToCommitLatencyAt().Observe(now.Sub(qc.Proposal().Timestamp()).Seconds())
		Global.commitRoadIndexAt().Set(int64(qc.Index()))                    // nolint: gosec
		Global.commitGlobalBlockNumberAt().Set(int64(qc.GlobalRange().Next)) // nolint: gosec
		*mLast = utils.Some(observed[*types.CommitQC]{now, qc})
	}
}

// ObserveProducedTxs counts txs included in a successfully produced local lane block.
func ObserveProducedTxs(n int) {
	Global.producedTxsAt().Add(int64(n))
}

// Metrics are the avail instruments callers write directly.
type Metrics struct {
	CommitRoadIndex         *prometheus.GaugeInt
	CommitGlobalBlockNumber *prometheus.GaugeInt
	LaneCapacityWait        *prometheus.Histogram
	LaneQCWait              *prometheus.Histogram
	LaneCapacityInFlight    *prometheus.GaugeInt
	LaneQCInFlight          *prometheus.GaugeInt
	LaneQCs                 *prometheus.CounterInt
	LaneVotesIngested       *prometheus.CounterInt
}

// Get returns the avail instruments.
func Get() *Metrics {
	return &Metrics{
		CommitRoadIndex:         Global.commitRoadIndexAt(),
		CommitGlobalBlockNumber: Global.commitGlobalBlockNumberAt(),
		LaneCapacityWait:        Global.laneCapacityWaitLatencyAt(),
		LaneQCWait:              Global.laneQcWaitLatencyAt(),
		LaneCapacityInFlight:    Global.inFlightAt("lane_capacity"),
		LaneQCInFlight:          Global.inFlightAt("lane_qc"),
		LaneQCs:                 Global.laneQcsAt(),
		LaneVotesIngested:       Global.laneVotesIngestedAt(),
	}
}
