package evmonly

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
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

	executionMetrics = struct {
		txsExecuted    metric.Int64Counter
		plainTransfers metric.Int64Counter
	}{
		txsExecuted: must(occMeter.Int64Counter(
			"giga_evmonly_txs_executed_total",
			metric.WithDescription("EVM-only transactions executed per block, by outcome (success, reverted, failed)"),
			metric.WithUnit("{transaction}"),
		)),
		plainTransfers: must(occMeter.Int64Counter(
			"giga_evmonly_plain_transfers_total",
			metric.WithDescription("EVM-only transactions applied as plain value transfers without the EVM, speculative executions included"),
			metric.WithUnit("{transaction}"),
		)),
	}

	occMetrics = struct {
		blocks     metric.Int64Counter
		fallbacks  metric.Int64Counter
		reruns     metric.Int64Counter
		conflicts  metric.Int64Counter
		rerunDepth metric.Int64Histogram
	}{
		blocks: must(occMeter.Int64Counter(
			"giga_occ_blocks_total",
			metric.WithDescription("Blocks executed by the Giga EVM-only executor, by execution outcome (parallel, fallback, sequential, empty)"),
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

const (
	txExecutionStatusSuccess  = "success"
	txExecutionStatusReverted = "reverted"
	txExecutionStatusRejected = "rejected"
	txExecutionStatusFailed   = "failed"
)

var txExecutionStatuses = []string{
	txExecutionStatusSuccess,
	txExecutionStatusReverted,
	txExecutionStatusRejected,
	txExecutionStatusFailed,
}

var (
	txExecutionStatusOptions = txExecutionStatusOptionTable()
)

func txExecutionStatusOptionTable() map[string]metric.MeasurementOption {
	table := make(map[string]metric.MeasurementOption, len(txExecutionStatuses))
	for _, status := range txExecutionStatuses {
		table[status] = metric.WithAttributes(attribute.String("status", status))
	}
	return table
}

func txExecutionStatusAttr(status string) metric.MeasurementOption {
	if option, ok := txExecutionStatusOptions[status]; ok {
		return option
	}
	return txExecutionStatusOptions[txExecutionStatusFailed]
}

// txExecutionStatus maps a transaction result onto the bounded status label
// vocabulary used by giga_evmonly_txs_executed_total.
func txExecutionStatus(tx TxResult) string {
	// A rejected transaction also carries the failed status.
	if tx.Rejected {
		return txExecutionStatusRejected
	}
	switch tx.Status {
	case ethtypes.ReceiptStatusSuccessful:
		return txExecutionStatusSuccess
	case ethtypes.ReceiptStatusFailed:
		return txExecutionStatusReverted
	default:
		// Catches a status the executor is not expected to produce.
		return txExecutionStatusFailed
	}
}

// recordTxExecutionStats emits per-transaction execution outcomes for a finished
// block, reporting every status in the vocabulary including the ones this block
// had none of.
//
// A counter series exists only once something has recorded to it, and a global
// instrument drops measurements taken before the meter provider is installed, so
// a status is only reachable by a query if a block reports it as zero. This runs
// inside block execution, so a telemetry fault must not panic into the caller.
func recordTxExecutionStats(ctx context.Context, txs []TxResult) {
	defer func() {
		if e := recover(); e != nil {
			fmt.Fprintf(os.Stderr, "telemetry panic: %v\n%s", e, debug.Stack())
		}
	}()
	counts := make(map[string]int64, len(txExecutionStatuses))
	for _, tx := range txs {
		counts[txExecutionStatus(tx)]++
	}
	for _, status := range txExecutionStatuses {
		executionMetrics.txsExecuted.Add(ctx, counts[status], txExecutionStatusAttr(status))
	}
}

// recordPlainTransfers emits how many transactions of a finished block were
// applied through the plain-transfer path.
func recordPlainTransfers(ctx context.Context, count uint64) {
	defer func() {
		if e := recover(); e != nil {
			fmt.Fprintf(os.Stderr, "telemetry panic: %v\n%s", e, debug.Stack())
		}
	}()
	executionMetrics.plainTransfers.Add(ctx, int64(count)) //nolint:gosec // bounded by the block's transaction count
}

// occLabelUnknown is the value every label vocabulary in this file collapses an
// unrecognized value onto.
const occLabelUnknown = "unknown"

// The outcome and reason label values are fixed at build time so the two
// dimensions of giga_occ_blocks_total and giga_occ_fallbacks_total stay the
// bounded sets the dashboards and the fallback-rate alert are built against.
var (
	occOutcomeParallel   = outcomeAttr("parallel")
	occOutcomeFallback   = outcomeAttr("fallback")
	occOutcomeSequential = outcomeAttr("sequential")
	occOutcomeEmpty      = outcomeAttr("empty")

	occReasonConflict         = reasonAttr(occFallbackReasonConflict)
	occReasonGasLimit         = reasonAttr(occFallbackReasonGasLimit)
	occReasonGasOverflow      = reasonAttr(occFallbackReasonGasOverflow)
	occReasonMaxIncarnation   = reasonAttr(occFallbackReasonMaxIncarnation)
	occReasonWorkerPoolClosed = reasonAttr(occFallbackReasonWorkerPoolClosed)
	occReasonUnknown          = reasonAttr(occLabelUnknown)
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

// occOutcome maps a block onto the outcome label vocabulary. Blocks that never
// tried optimistic execution are split by whether they carried transactions at
// all, so "sequential" counts only blocks that had work an optimistic run could
// have parallelized: FinalizeBlock runs at every height, and folding empty
// blocks in would make parallel/total decay with block rate on an idle chain
// rather than with OCC behavior.
func occOutcome(txCount int, stats OCCStats) metric.MeasurementOption {
	switch {
	case stats.Attempted && stats.Fallback:
		return occOutcomeFallback
	case stats.Attempted:
		return occOutcomeParallel
	case txCount == 0:
		return occOutcomeEmpty
	default:
		return occOutcomeSequential
	}
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

// occConflictLabel names one of the bounded access-by-kind series of
// giga_occ_conflicts_total.
type occConflictLabel struct {
	access string
	kind   string
}

// The conflict label values are fixed at build time for the same reason the
// outcome and reason values are, and one measurement option is pre-built per
// series so emitting a block's conflicts allocates no attribute sets.
var (
	occConflictAccesses = occLabelVocabulary("read", "write")
	occConflictKinds    = occLabelVocabulary("account", "balance", "nonce", "code", "storage")
	occConflictOptions  = occConflictOptionTable()
)

// occLabelVocabulary returns the given label values plus "unknown" as a set.
func occLabelVocabulary(values ...string) map[string]struct{} {
	vocabulary := make(map[string]struct{}, len(values)+1)
	vocabulary[occLabelUnknown] = struct{}{}
	for _, value := range values {
		vocabulary[value] = struct{}{}
	}
	return vocabulary
}

func occConflictOptionTable() map[occConflictLabel]metric.MeasurementOption {
	table := make(map[occConflictLabel]metric.MeasurementOption, len(occConflictAccesses)*len(occConflictKinds))
	for access := range occConflictAccesses {
		for kind := range occConflictKinds {
			table[occConflictLabel{access: access, kind: kind}] = metric.WithAttributes(
				attribute.String("access", access),
				attribute.String("kind", kind),
			)
		}
	}
	return table
}

// occConflictKey maps a conflict sample onto the label vocabulary. Mapping here
// rather than passing the sample's strings through is what keeps the series
// closed, the same way occReason does for the fallback reason: it does not rely
// on stateAccessKind.String() staying bounded.
func occConflictKey(access, kind string) occConflictLabel {
	return occConflictLabel{
		access: occLabel(occConflictAccesses, access),
		kind:   occLabel(occConflictKinds, kind),
	}
}

// occLabel returns value when the vocabulary holds it, and "unknown" otherwise.
func occLabel(vocabulary map[string]struct{}, value string) string {
	if _, ok := vocabulary[value]; ok {
		return value
	}
	return occLabelUnknown
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
func recordOCCStats(ctx context.Context, txCount int, stats OCCStats) {
	defer func() {
		if e := recover(); e != nil {
			fmt.Fprintf(os.Stderr, "telemetry panic: %v\n%s", e, debug.Stack())
		}
	}()
	occMetrics.blocks.Add(ctx, 1, occOutcome(txCount, stats))
	if !stats.Attempted {
		return
	}
	if stats.Fallback {
		occMetrics.fallbacks.Add(ctx, 1, occReason(stats.FallbackReason))
	}
	occMetrics.rerunDepth.Record(ctx, utils.Clamp[int64](stats.MaxIncarnation))
	if stats.RerunCount > 0 {
		occMetrics.reruns.Add(ctx, utils.Clamp[int64](stats.RerunCount))
	}
	recordOCCConflicts(ctx, stats.ConflictSamples)
}

// recordOCCConflicts sums a block's conflict samples onto the access-by-kind
// series before emitting them. ConflictSamples holds one entry per distinct
// (access, kind, address, slot) and is uncapped, so emitting per sample would
// scale this block's telemetry work with the number of conflicting state keys;
// aggregating first bounds it by the size of the label space instead.
func recordOCCConflicts(ctx context.Context, samples []OCCConflictCount) {
	if len(samples) == 0 {
		return
	}
	counts := make(map[occConflictLabel]uint64, min(len(samples), len(occConflictOptions)))
	for _, sample := range samples {
		counts[occConflictKey(sample.Access, sample.Kind)] += sample.Count
	}
	for label, count := range counts {
		occMetrics.conflicts.Add(ctx, utils.Clamp[int64](count), occConflictOptions[label])
	}
}
