package metrics

import (
	"strconv"
	"time"

	dto "github.com/prometheus/client_model/go"

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

	// Global block number of the highest observed commitQC.
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
		SetCommitRoadIndex(qc.Index())
		SetCommitGlobalBlockNumber(qc.GlobalRange().Next)
		*mLast = utils.Some(observed[*types.CommitQC]{now, qc})
	}
}

// ObserveProducedTxs counts txs included in a successfully produced local lane block.
func ObserveProducedTxs(n int) {
	Global.producedTxsAt().Add(int64(n))
}

// SetCommitRoadIndex records the road index of the highest observed commitQC.
func SetCommitRoadIndex(idx types.RoadIndex) {
	Global.commitRoadIndexAt().Set(int64(idx)) // nolint: gosec
}

// CommitRoadIndex returns the road index recorded for the highest observed commitQC.
func CommitRoadIndex() int64 {
	return gaugeValue(Global.commitRoadIndexAt())
}

// SetCommitGlobalBlockNumber records the global block number of the highest observed commitQC.
func SetCommitGlobalBlockNumber(n types.GlobalBlockNumber) {
	Global.commitGlobalBlockNumberAt().Set(int64(n)) // nolint: gosec
}

// CommitGlobalBlockNumber returns the global block number recorded for the highest observed commitQC.
func CommitGlobalBlockNumber() int64 {
	return gaugeValue(Global.commitGlobalBlockNumberAt())
}

func gaugeValue(g *prometheus.GaugeInt) int64 {
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		return 0
	}
	return int64(m.GetGauge().GetValue())
}

// ObserveLaneCapacityWait records how long one WaitForCapacity call blocked.
// A call that finds room immediately is not recorded. A call canceled while blocked is.
func ObserveLaneCapacityWait(d time.Duration) {
	Global.laneCapacityWaitLatencyAt().Observe(d.Seconds())
}

func enterWait(wait string) func() {
	g := Global.inFlightAt(wait)
	g.Add(1)
	return func() { g.Add(-1) }
}

// EnterLaneCapacityWait marks a WaitForCapacity call as in flight.
func EnterLaneCapacityWait() func() { return enterWait("lane_capacity") }

// EnterLaneQCWait marks a WaitForLaneQCs call as in flight.
func EnterLaneQCWait() func() { return enterWait("lane_qc") }

// ObserveLaneQCWait records how long one WaitForLaneQCs call blocked.
// A call that finds a LaneQC immediately is not recorded. A call canceled while blocked is.
func ObserveLaneQCWait(d time.Duration) {
	Global.laneQcWaitLatencyAt().Observe(d.Seconds())
}

// ObserveLaneQC records that lane votes reached quorum and formed a LaneQC.
func ObserveLaneQC() {
	Global.laneQcsAt().Add(1)
}

// ObserveLaneVoteIngested records that a lane vote was newly stored.
func ObserveLaneVoteIngested() {
	Global.laneVotesIngestedAt().Add(1)
}

// LaneVotesIngested returns the number of lane votes recorded as newly stored.
func LaneVotesIngested() int64 {
	var m dto.Metric
	if err := Global.laneVotesIngestedAt().Write(&m); err != nil {
		return 0
	}
	return int64(m.GetCounter().GetValue())
}

// LaneQCs returns the number of LaneQCs recorded as formed.
func LaneQCs() int64 {
	var m dto.Metric
	if err := Global.laneQcsAt().Write(&m); err != nil {
		return 0
	}
	return int64(m.GetCounter().GetValue())
}
