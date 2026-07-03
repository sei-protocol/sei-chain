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
	// Road index of the highest observed appQC.
	appRoadIndex prometheus.GaugeIntVec

	// Global block number of the highest observed commitQC.
	commitGlobalBlockNumber prometheus.GaugeIntVec
	// Global block number of the highest observed appQC.
	appGlobalBlockNumber prometheus.GaugeIntVec

	// Latency from proposal being constructed to commit being observed.
	proposalToCommitLatency prometheus.HistogramVec `metrics_buckets:"exp(0.01, 1.2, 35)"`
	// Latency between consecutive commits being observed.
	commitToCommitLatency prometheus.HistogramVec `metrics_labels:"timeouts" metrics_buckets:"none"`
}

type observed[T any] struct {
	time time.Time
	val  T
}

func newObserved[T any]() utils.Mutex[*utils.Option[observed[T]]] {
	return utils.NewMutex(utils.Alloc(utils.None[observed[T]]()))
}

var observedCommitQC = newObserved[*types.CommitQC]()
var observedAppQC = newObserved[*types.AppQC]()

// ObserveCommitQC observes the CommitQC latency.
func ObserveCommitQC(c *types.Committee, qc *types.CommitQC) {
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
		Global.commitRoadIndexAt().Set(int64(qc.Index()))                     // nolint: gosec
		Global.commitGlobalBlockNumberAt().Set(int64(qc.GlobalRange(c).Next)) // nolint: gosec
		*mLast = utils.Some(observed[*types.CommitQC]{now, qc})
	}
}

func ObserveAppQC(qc *types.AppQC) {
	now := time.Now()
	for mLast := range observedAppQC.Lock() {
		if last, ok := mLast.Get(); ok && last.val.Proposal().GlobalNumber() >= qc.Proposal().GlobalNumber() {
			return
		}
		Global.appRoadIndexAt().Set(int64(qc.Proposal().RoadIndex())) // nolint: gosec
		// +1 is for consistency with commitGlobalBlockNumber
		Global.appGlobalBlockNumberAt().Set(int64(qc.Proposal().GlobalNumber() + 1)) // nolint: gosec
		*mLast = utils.Some(observed[*types.AppQC]{now, qc})
	}
}
