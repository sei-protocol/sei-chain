package metrics

import (
	"context"
	"time"

	prometheus "github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func buckets(start float64, factor float64, count int) metric.HistogramOption {
	return metric.WithExplicitBucketBoundaries(prometheus.ExponentialBuckets(start, factor, count)...)
}

var meter = otel.Meter("tendermint_internal_autobahn_avail")

var commitRoadIndex = utils.OrPanic1(meter.Int64Gauge(
	"tendermint_internal_autobahn_avail__commit_road_index",
	metric.WithDescription("road index of the highest observed commitQC"),
))
var appRoadIndex = utils.OrPanic1(meter.Int64Gauge(
	"tendermint_internal_autobahn_avail__app_road_index",
	metric.WithDescription("road index of the highest observed appQC"),
))
var commitGlobalBlockNumber = utils.OrPanic1(meter.Int64Gauge(
	"tendermint_internal_autobahn_avail__commit_global_block_number",
	metric.WithDescription("global block number of the highest observed commitQC"),
))
var appGlobalBlockNumber = utils.OrPanic1(meter.Int64Gauge(
	"tendermint_internal_autobahn_avail__app_global_block_number",
	metric.WithDescription("global block number of the highest observed appQC"),
))
var proposalToCommitLatency = utils.OrPanic1(meter.Float64Histogram(
	"tendermint_internal_autobahn_avail__proposal_to_commit_latency",
	buckets(0.01, 1.2, 35),
	metric.WithDescription("latency from proposal being constructed to commit being observed"),
))
var commitToCommitLatencySum = utils.OrPanic1(meter.Float64Counter(
	"tendermint_internal_autobahn_avail__commit_to_commit_latency_sum",
	metric.WithDescription("latency between consecutive commits being observed (SUM)"),
))
var commitToCommitLatencyCount = utils.OrPanic1(meter.Int64Counter(
	"tendermint_internal_autobahn_avail__commit_to_commit_latency_count",
	metric.WithDescription("latency between consecutive commits being observed (COUNT)"),
))

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
	ctx := context.Background()
	now := time.Now()
	for mLast := range observedCommitQC.Lock() {
		if last, ok := mLast.Get(); ok {
			if last.val.Index() >= qc.Index() {
				return
			}
			// Constructed once per CommitQC, which we should afford.
			attrs := metric.WithAttributeSet(attribute.NewSet(
				// Timeouts capped: 20 means [20,inf)
				attribute.Int64("timeouts", int64(min(qc.Proposal().View().Number, 20))), // nolint: gosec
			))
			commitToCommitLatencySum.Add(ctx, now.Sub(last.time).Seconds(), attrs)
			commitToCommitLatencyCount.Add(ctx, 1, attrs)
		}
		proposalToCommitLatency.Record(ctx, now.Sub(qc.Proposal().Timestamp()).Seconds())
		commitRoadIndex.Record(ctx, int64(qc.Index()))                     // nolint: gosec
		commitGlobalBlockNumber.Record(ctx, int64(qc.GlobalRange(c).Next)) // nolint: gosec
		*mLast = utils.Some(observed[*types.CommitQC]{now, qc})
	}
}

func ObserveAppQC(qc *types.AppQC) {
	ctx := context.Background()
	now := time.Now()
	for mLast := range observedAppQC.Lock() {
		if last, ok := mLast.Get(); ok && last.val.Proposal().GlobalNumber() >= qc.Proposal().GlobalNumber() {
			return
		}
		appRoadIndex.Record(ctx, int64(qc.Proposal().RoadIndex())) // nolint: gosec
		// +1 is for consistency with commitGlobalBlockNumber
		appGlobalBlockNumber.Record(ctx, int64(qc.Proposal().GlobalNumber()+1)) // nolint: gosec
		*mLast = utils.Some(observed[*types.AppQC]{now, qc})
	}
}
