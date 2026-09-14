package evmonly

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// occMeter carries the OCC instruments emitted by every executor. The node
// binds the global meter provider with a "sei_chain" Prometheus namespace, so
// giga_occ_blocks_total is scraped as sei_chain_giga_occ_blocks_total.
var (
	occMeter = otel.Meter("giga_evmonly")

	occMetrics = struct {
		blocks     metric.Int64Counter
		fallbacks  metric.Int64Counter
		reruns     metric.Int64Counter
		conflicts  metric.Int64Counter
		rerunDepth metric.Int64Histogram
	}{
		blocks: must(occMeter.Int64Counter(
			"giga_occ_blocks_total",
			metric.WithDescription("Blocks executed by the Giga EVM-only executor, by execution outcome (parallel, fallback, sequential)"),
			metric.WithUnit("{block}"),
		)),
		fallbacks: must(occMeter.Int64Counter(
			"giga_occ_fallbacks_total",
			metric.WithDescription("Blocks that abandoned optimistic execution and re-ran sequentially, by fallback reason"),
			metric.WithUnit("{block}"),
		)),
		reruns: must(occMeter.Int64Counter(
			"giga_occ_reruns_total",
			metric.WithDescription("Transaction reruns scheduled by optimistic execution validation"),
			metric.WithUnit("{count}"),
		)),
		conflicts: must(occMeter.Int64Counter(
			"giga_occ_conflicts_total",
			metric.WithDescription("State access conflicts observed while validating optimistic execution, by access and state kind"),
			metric.WithUnit("{count}"),
		)),
		rerunDepth: must(occMeter.Int64Histogram(
			"giga_occ_rerun_depth",
			metric.WithDescription("Deepest transaction incarnation reached per optimistically executed block"),
			metric.WithUnit("{incarnation}"),
			metric.WithExplicitBucketBoundaries(occRerunDepthBuckets()...),
		)),
	}
)

// The outcome and reason label values are fixed at build time so the two
// dimensions of giga_occ_blocks_total and giga_occ_fallbacks_total stay the
// bounded sets the dashboards and the fallback-rate alert are built against.
var (
	occOutcomeParallel   = outcomeAttr("parallel")
	occOutcomeFallback   = outcomeAttr("fallback")
	occOutcomeSequential = outcomeAttr("sequential")

	occReasonConflict         = reasonAttr(occFallbackReasonConflict)
	occReasonGasLimit         = reasonAttr(occFallbackReasonGasLimit)
	occReasonGasOverflow      = reasonAttr(occFallbackReasonGasOverflow)
	occReasonMaxIncarnation   = reasonAttr(occFallbackReasonMaxIncarnation)
	occReasonWorkerPoolClosed = reasonAttr(occFallbackReasonWorkerPoolClosed)
	occReasonUnknown          = reasonAttr("unknown")
)

func outcomeAttr(outcome string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("outcome", outcome))
}

func reasonAttr(reason string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("reason", reason))
}

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}

// occRerunDepthBuckets returns one bucket per reachable incarnation, starting at
// 0 so a block with no reruns is counted apart from one that reran a
// transaction once. A rerun past occMaxTxIncarnations falls back instead, so the
// deepest incarnation any block reaches is one below it.
func occRerunDepthBuckets() []float64 {
	buckets := make([]float64, 0, occMaxTxIncarnations)
	for depth := 0; depth < occMaxTxIncarnations; depth++ {
		buckets = append(buckets, float64(depth))
	}
	return buckets
}

// occReason maps a fallback reason onto the label vocabulary. Mapping here
// rather than passing the string through is what keeps the vocabulary closed: a
// reason this function does not know collapses to "unknown" instead of adding a
// series nothing queries.
func occReason(reason string) metric.MeasurementOption {
	switch reason {
	case occFallbackReasonConflict:
		return occReasonConflict
	case occFallbackReasonGasLimit:
		return occReasonGasLimit
	case occFallbackReasonGasOverflow:
		return occReasonGasOverflow
	case occFallbackReasonMaxIncarnation:
		return occReasonMaxIncarnation
	case occFallbackReasonWorkerPoolClosed:
		return occReasonWorkerPoolClosed
	default:
		return occReasonUnknown
	}
}

// recordOCCStats emits a block's optimistic concurrency behavior on the global
// meter. Every block records an outcome, including the ones that never tried
// optimistic execution, so the fallback rate has a full denominator.
//
// Conflict addresses and slots are deliberately left off the labels: they are
// per-contract and per-slot, and would make the series unbounded. They stay in
// OCCStats.ConflictSamples for logs and spans.
//
// This runs inside block execution, so a telemetry fault must not panic into
// the caller.
func recordOCCStats(ctx context.Context, stats OCCStats) {
	defer func() {
		if e := recover(); e != nil {
			fmt.Fprintf(os.Stderr, "telemetry panic: %v\n%s", e, debug.Stack())
		}
	}()
	if !stats.Attempted {
		occMetrics.blocks.Add(ctx, 1, occOutcomeSequential)
		return
	}
	if stats.Fallback {
		occMetrics.blocks.Add(ctx, 1, occOutcomeFallback)
		occMetrics.fallbacks.Add(ctx, 1, occReason(stats.FallbackReason))
	} else {
		occMetrics.blocks.Add(ctx, 1, occOutcomeParallel)
	}
	occMetrics.rerunDepth.Record(ctx, utils.Clamp[int64](stats.MaxIncarnation))
	if stats.RerunCount > 0 {
		occMetrics.reruns.Add(ctx, utils.Clamp[int64](stats.RerunCount))
	}
	for _, conflict := range stats.ConflictSamples {
		occMetrics.conflicts.Add(ctx, utils.Clamp[int64](conflict.Count), metric.WithAttributes(
			attribute.String("access", conflict.Access),
			attribute.String("kind", conflict.Kind),
		))
	}
}
