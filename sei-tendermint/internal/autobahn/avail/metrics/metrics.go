package metrics

import (
	"time"
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	prometheus "github.com/prometheus/client_golang/prometheus"
	
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

func buckets(start float64, factor float64, count int) metric.HistogramOption {
	return metric.WithExplicitBucketBoundaries(prometheus.ExponentialBuckets(start, factor, count)...)
}

var meter = otel.Meter("tendermint_internal_autobahn_consensus")

var proposalToCommitLatency = utils.OrPanic1(meter.Float64Histogram(
	"proposal_to_commit_latency",
	buckets(0.01, 1.2, 35),
	metric.WithDescription("latency from proposal being constructed to commit being observed"),
))
var commitToCommitLatencySum = utils.OrPanic1(meter.Float64Counter(
	"commit_to_commit_latency_sum",
	metric.WithDescription("latency between consecutive commits being observed (SUM)"),
))
var commitToCommitLatencyCount = utils.OrPanic1(meter.Int64Counter(
	"commit_to_commit_latency_count",
	metric.WithDescription("latency between consecutive commits being observed (COUNT)"),
))

var lastObserveCommitQC = utils.NewMutex(utils.Alloc(utils.None[time.Time]()))

// ObserveCommitQC observes the CommitQC latency.
func ObserveCommitQC(qc *types.CommitQC) {
	ctx := context.Background()
	now := time.Now()
	proposalToCommitLatency.Record(ctx, now.Sub(qc.Proposal().Timestamp()).Seconds())
	for mLast := range lastObserveCommitQC.Lock() {
		if last, ok := mLast.Get(); ok {
			// Constructed once per CommitQC, which we should afford.
			attrs := metric.WithAttributeSet(attribute.NewSet(
				attribute.Int64("timeouts",int64(qc.Proposal().View().Number)),
			))
			commitToCommitLatencySum.Add(ctx, now.Sub(last).Seconds(), attrs)
			commitToCommitLatencyCount.Add(ctx, 1, attrs)
		}
		*mLast = utils.Some(now)
	}
}
