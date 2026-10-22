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
	// Lane blocks this node holds, by the consensus step they are waiting on.
	laneBlocksInFlight prometheus.GaugeIntVec `metrics_labels:"stage"`
	// LaneQCs formed by lane votes reaching quorum.
	laneQcs prometheus.CounterIntVec
	// Lane votes newly stored.
	laneVotesIngested prometheus.CounterIntVec
	// Latency from a lane block's creation to the given stage, observed by a node holding the block.
	laneBlockLatency prometheus.HistogramVec `metrics_labels:"stage" metrics_buckets:"exp(0.001, 1.5, 30)"`
	// Latency from a proposal's construction to this node casting its PrepareVote or observing its PrepareQC or CommitQC.
	proposalLatency prometheus.HistogramVec `metrics_labels:"stage" metrics_buckets:"exp(0.0005, 1.5, 30)"`
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
		Global.proposalLatencyAt("commit_qc").Observe(now.Sub(qc.Proposal().Timestamp()).Seconds())
		Global.commitRoadIndexAt().Set(int64(qc.Index()))                    // nolint: gosec
		Global.commitGlobalBlockNumberAt().Set(int64(qc.GlobalRange().Next)) // nolint: gosec
		*mLast = utils.Some(observed[*types.CommitQC]{now, qc})
	}
}

// ObservePrepareVote records how long after p's construction this node cast its PrepareVote for it.
// Call it once per view.
func ObservePrepareVote(p *types.Proposal) {
	Global.proposalLatencyAt("prepare_vote").Observe(time.Since(p.Timestamp()).Seconds())
}

// ObservePrepareQC records how long after its proposal's construction a PrepareQC was observed.
// Call it once per view.
func ObservePrepareQC(qc *types.PrepareQC) {
	Global.proposalLatencyAt("prepare_qc").Observe(time.Since(qc.Proposal().Timestamp()).Seconds())
}

// ObserveProducedTxs counts txs included in a successfully produced local lane block.
func ObserveProducedTxs(n int) {
	Global.producedTxsAt().Add(int64(n))
}

// LaneBlockStages counts the lane blocks a node holds by the consensus step each is waiting on.
type LaneBlockStages struct {
	LaneQC    int64
	Proposal  int64
	PrepareQC int64
	CommitQC  int64
}

// ObserveLaneBlockStages publishes s as lane_blocks_in_flight{stage}.
func ObserveLaneBlockStages(s LaneBlockStages) {
	Global.laneBlocksInFlightAt("lane_qc").Set(s.LaneQC)
	Global.laneBlocksInFlightAt("proposal").Set(s.Proposal)
	Global.laneBlocksInFlightAt("prepare_qc").Set(s.PrepareQC)
	Global.laneBlocksInFlightAt("commit_qc").Set(s.CommitQC)
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
	// Latency from a lane block's creation to: the block arriving from its producer, a lane vote on
	// it arriving, its LaneQC forming, and the leader including it in a proposal.
	LaneBlockReceive  *prometheus.Histogram
	LaneVoteReceive   *prometheus.Histogram
	LaneQCFormed      *prometheus.Histogram
	LaneBlockProposed *prometheus.Histogram
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
		LaneBlockReceive:        Global.laneBlockLatencyAt("block_receive"),
		LaneVoteReceive:         Global.laneBlockLatencyAt("vote_receive"),
		LaneQCFormed:            Global.laneBlockLatencyAt("lane_qc"),
		LaneBlockProposed:       Global.laneBlockLatencyAt("proposal"),
	}
}
