package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// bindTestOCCMetrics rebuilds the package OCC instruments against a manual
// reader and restores the originals when the test ends. The package binds its
// instruments to the global meter at init, and the global provider accepts only
// the first delegate installed in a test binary, so swapping the instruments is
// what lets each test collect only its own measurements.
func bindTestOCCMetrics(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("giga_evmonly")
	previous := occMetrics
	t.Cleanup(func() { occMetrics = previous })
	occMetrics.blocks = must(meter.Int64Counter("giga_occ_blocks_total"))
	occMetrics.fallbacks = must(meter.Int64Counter("giga_occ_fallbacks_total"))
	occMetrics.reruns = must(meter.Int64Counter("giga_occ_reruns_total"))
	occMetrics.conflicts = must(meter.Int64Counter("giga_occ_conflicts_total"))
	occMetrics.rerunDepth = must(meter.Int64Histogram(
		"giga_occ_rerun_depth",
		metric.WithExplicitBucketBoundaries(occRerunDepthBuckets()...),
	))
	return reader
}

// collectOCCMetrics returns the collected metrics keyed by name. A metric with
// no measurements is absent from the map rather than present and empty.
func collectOCCMetrics(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	collected := map[string]metricdata.Metrics{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			collected[m.Name] = m
		}
	}
	return collected
}

// requireCounter returns the value of the named counter's single data point
// carrying every given attribute.
func requireCounter(t *testing.T, collected map[string]metricdata.Metrics, name string, attrs ...attribute.KeyValue) int64 {
	t.Helper()
	m, ok := collected[name]
	require.True(t, ok, "expected counter %s to be collected", name)
	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "expected %s to be an int64 sum", name)
	var matched []metricdata.DataPoint[int64]
	for _, point := range sum.DataPoints {
		if hasAllAttributes(point.Attributes, attrs) {
			matched = append(matched, point)
		}
	}
	require.Len(t, matched, 1, "expected exactly one %s data point for %v", name, attrs)
	return matched[0].Value
}

func hasAllAttributes(set attribute.Set, attrs []attribute.KeyValue) bool {
	for _, want := range attrs {
		got, ok := set.Value(want.Key)
		if !ok || got != want.Value {
			return false
		}
	}
	return true
}

// requireHistogram returns the named histogram's single data point.
func requireHistogram(t *testing.T, collected map[string]metricdata.Metrics, name string) metricdata.HistogramDataPoint[int64] {
	t.Helper()
	m, ok := collected[name]
	require.True(t, ok, "expected histogram %s to be collected", name)
	histogram, ok := m.Data.(metricdata.Histogram[int64])
	require.True(t, ok, "expected %s to be an int64 histogram", name)
	require.Len(t, histogram.DataPoints, 1)
	return histogram.DataPoints[0]
}

// conflictingTransferBlock returns a block whose transactions all credit the
// same recipient, so optimistic execution observes balance conflicts and reruns
// transactions without falling back.
func conflictingTransferBlock(t *testing.T, recipient common.Address, txCount int) (BlockRequest, *MemoryState) {
	t.Helper()
	chainID := big.NewInt(testChainID)
	state := NewMemoryState()
	rawTxs := make([][]byte, 0, txCount)
	for range txCount {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), big.NewInt(1_000_000))
		rawTxs = append(rawTxs, signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(3), nil, 100_000, big.NewInt(0)))
	}
	return BlockRequest{Context: blockContext(chainID), Txs: rawTxs}, state
}

func TestRecordOCCStatsParallelBlockReportsRerunsAndConflicts(t *testing.T) {
	reader := bindTestOCCMetrics(t)
	req, state := conflictingTransferBlock(t, testAddress(0xdd), 8)

	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 4}, withTestState(state))
	result, err := executor.ExecuteBlock(t.Context(), req)
	require.NoError(t, err)
	require.True(t, result.OCCStats.Attempted)
	require.False(t, result.OCCStats.Fallback)
	require.Greater(t, result.OCCStats.RerunCount, uint64(0))

	collected := collectOCCMetrics(t, reader)
	require.Equal(t, int64(1), requireCounter(t, collected, "giga_occ_blocks_total", attribute.String("outcome", "parallel")))
	require.NotContains(t, collected, "giga_occ_fallbacks_total")

	reruns := requireCounter(t, collected, "giga_occ_reruns_total")
	require.Equal(t, int64(result.OCCStats.RerunCount), reruns)

	conflicts := requireCounter(t, collected, "giga_occ_conflicts_total",
		attribute.String("access", "read"),
		attribute.String("kind", "balance"),
	)
	require.Greater(t, conflicts, int64(0))

	depth := requireHistogram(t, collected, "giga_occ_rerun_depth")
	require.Equal(t, uint64(1), depth.Count)
	require.Equal(t, int64(result.OCCStats.MaxIncarnation), depth.Sum)
	require.GreaterOrEqual(t, depth.Sum, int64(1))
}

// TestRecordOCCStatsConflictLabelsOmitAddressAndSlot pins the cardinality
// contract: conflicts are reported per access and state kind, never per
// contract address or storage slot.
func TestRecordOCCStatsConflictLabelsOmitAddressAndSlot(t *testing.T) {
	reader := bindTestOCCMetrics(t)
	recipient := testAddress(0xdd)
	req, state := conflictingTransferBlock(t, recipient, 8)

	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 4}, withTestState(state))
	result, err := executor.ExecuteBlock(t.Context(), req)
	require.NoError(t, err)

	// The samples carry the address, so the assertions below prove the emitter
	// dropped it rather than that there was nothing to drop.
	sampledRecipient := false
	for _, conflict := range result.OCCStats.ConflictSamples {
		if conflict.Address == recipient {
			sampledRecipient = true
		}
	}
	require.True(t, sampledRecipient, "fixture must produce a conflict on the shared recipient")

	collected := collectOCCMetrics(t, reader)
	conflicts, ok := collected["giga_occ_conflicts_total"]
	require.True(t, ok)
	sum, ok := conflicts.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.NotEmpty(t, sum.DataPoints)
	for _, point := range sum.DataPoints {
		keys := make([]string, 0, point.Attributes.Len())
		for _, attr := range point.Attributes.ToSlice() {
			keys = append(keys, string(attr.Key))
			require.NotContains(t, attr.Value.Emit(), recipient.Hex())
		}
		require.ElementsMatch(t, []string{"access", "kind"}, keys)
	}
}

func TestRecordOCCStatsSequentialBlockReportsNoOCCActivity(t *testing.T) {
	reader := bindTestOCCMetrics(t)
	req, state := conflictingTransferBlock(t, testAddress(0xde), 1)

	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 4}, withTestState(state))
	result, err := executor.ExecuteBlock(t.Context(), req)
	require.NoError(t, err)
	require.False(t, result.OCCStats.Attempted)

	collected := collectOCCMetrics(t, reader)
	require.Equal(t, int64(1), requireCounter(t, collected, "giga_occ_blocks_total", attribute.String("outcome", "sequential")))
	require.NotContains(t, collected, "giga_occ_fallbacks_total")
	require.NotContains(t, collected, "giga_occ_reruns_total")
	require.NotContains(t, collected, "giga_occ_conflicts_total")
	require.NotContains(t, collected, "giga_occ_rerun_depth")
}

func TestRecordOCCStatsFallbackBlockReportsReason(t *testing.T) {
	reader := bindTestOCCMetrics(t)
	req, state := conflictingTransferBlock(t, testAddress(0xdf), 2)

	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 2}, withTestState(state))
	require.NotNil(t, executor.occPool)
	executor.occPool.Close()
	result, err := executor.ExecuteBlock(t.Context(), req)
	require.NoError(t, err)
	require.True(t, result.OCCStats.Fallback)

	collected := collectOCCMetrics(t, reader)
	require.Equal(t, int64(1), requireCounter(t, collected, "giga_occ_blocks_total", attribute.String("outcome", "fallback")))
	require.Equal(t, int64(1), requireCounter(t, collected, "giga_occ_fallbacks_total",
		attribute.String("reason", occFallbackReasonWorkerPoolClosed),
	))
}

// TestRecordOCCStatsFallbackReasonVocabularyIsClosed pins that the reason label
// can only take the reasons the executor defines, so a reason the emitter does
// not know cannot grow a series the dashboards do not know about.
func TestRecordOCCStatsFallbackReasonVocabularyIsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason string
		want   string
	}{
		{name: "conflict", reason: occFallbackReasonConflict, want: "conflict"},
		{name: "gas limit", reason: occFallbackReasonGasLimit, want: "gas_limit"},
		{name: "gas overflow", reason: occFallbackReasonGasOverflow, want: "gas_overflow"},
		{name: "max incarnation", reason: occFallbackReasonMaxIncarnation, want: "max_incarnation"},
		{name: "worker pool closed", reason: occFallbackReasonWorkerPoolClosed, want: "worker_pool_closed"},
		{name: "empty", reason: "", want: "unknown"},
		{name: "unrecognized", reason: "a_reason_nobody_defined", want: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := bindTestOCCMetrics(t)
			recordOCCStats(t.Context(), OCCStats{Attempted: true, Fallback: true, FallbackReason: tc.reason})

			collected := collectOCCMetrics(t, reader)
			require.Equal(t, int64(1), requireCounter(t, collected, "giga_occ_fallbacks_total",
				attribute.String("reason", tc.want),
			))
		})
	}
}

// TestRecordOCCStatsRerunDepthRecordsEveryAttemptedBlock keeps the histogram's
// count equal to the number of blocks that tried optimistic execution, so its
// quantiles describe those blocks and not only the ones that reran.
func TestRecordOCCStatsRerunDepthRecordsEveryAttemptedBlock(t *testing.T) {
	reader := bindTestOCCMetrics(t)
	recordOCCStats(t.Context(), OCCStats{Attempted: true})
	recordOCCStats(t.Context(), OCCStats{Attempted: true, RerunCount: 4, MaxIncarnation: 3})

	collected := collectOCCMetrics(t, reader)
	depth := requireHistogram(t, collected, "giga_occ_rerun_depth")
	require.Equal(t, uint64(2), depth.Count)
	require.Equal(t, int64(3), depth.Sum)
	require.Equal(t, int64(4), requireCounter(t, collected, "giga_occ_reruns_total"))
}
