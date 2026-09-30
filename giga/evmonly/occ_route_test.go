package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// speculate runs the block's transactions against its start state and returns the speculative results.
func speculate(t *testing.T, req PreparedBlock, state StateReader) []occTxExecution {
	t.Helper()
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 4})
	defer executor.Close()
	results := make([]occTxExecution, len(req.Txs))
	runner := newOCCSpeculativeRunner(executor, req)
	require.NoError(t, runner.runRanges(t.Context(), executor.occPool, occRanges(len(req.Txs), 16), state, runner.blockGasLimit, results))
	return results
}

func TestMeasureDependencies(t *testing.T) {
	const n = 256
	want := map[string]occDependencies{
		"independent":            {dependent: 0, depth: 1},
		"one_hot_slot":           {dependent: n - 1, depth: n},
		"hot_slots_2":            {dependent: n - 2, depth: n / 2},
		"hot_slots_4":            {dependent: n - 4, depth: n / 4},
		"hot_slots_16":           {dependent: n - 16, depth: n / 16},
		"hot_slots_64":           {dependent: n - 64, depth: n / 64},
		"hot_slots_256":          {dependent: 0, depth: 1},
		"one_sender":             {dependent: n - 1, depth: n},
		"independent_eighth_hot": {dependent: n/8 - 1, depth: n / 8},
		"hot_prefix":             {dependent: n - 1, depth: n},
		"hot_tail":               {dependent: 0, depth: 1},
	}
	for _, shape := range routeShapes() {
		t.Run(shape.name, func(t *testing.T) {
			req, state := buildRouteBlock(t, shape, n)
			require.Equal(t, want[shape.name], measureDependencies(speculate(t, req, state)))
		})
	}
}

// TestRouteAfterSpeculation requires, for blocks of one sample, the sequential path for blocks whose
// transactions chain through shared keys and the frontier for blocks whose transactions are independent or
// mostly so.
func TestRouteAfterSpeculation(t *testing.T) {
	want := map[string]occPath{
		"independent":            occPathFrontier,
		"one_hot_slot":           occPathSequential,
		"hot_slots_2":            occPathSequential,
		"hot_slots_4":            occPathSequential,
		"hot_slots_16":           occPathSequential,
		"hot_slots_64":           occPathSequential,
		"hot_slots_256":          occPathFrontier,
		"one_sender":             occPathSequential,
		"independent_eighth_hot": occPathFrontier,
		"hot_prefix":             occPathSequential,
		"hot_tail":               occPathFrontier,
	}
	for _, shape := range routeShapes() {
		t.Run(shape.name, func(t *testing.T) {
			req, state := buildRouteBlock(t, shape, occDependencySample)
			require.Equal(t, want[shape.name], routeAfterSpeculation(speculate(t, req, state)))
		})
	}
}

func TestOCCDependencySpans(t *testing.T) {
	for _, n := range []int{0, 1, occDependencySample - 1, occDependencySample, occDependencySample + 1, 300, 2000, 100_000} {
		spans := occDependencySpans(n)
		sampled := 0
		for _, span := range spans {
			sampled += span.end - span.start
		}
		require.Equal(t, min(n, occDependencySample), sampled, "n=%d", n)
		require.Equal(t, 0, spans[0].start, "n=%d", n)
		require.Equal(t, n, spans[len(spans)-1].end, "n=%d", n)
		covered := make([]int, n)
		for _, span := range append(spans, occSpansOutside(spans, n)...) {
			for i := span.start; i < span.end; i++ {
				covered[i]++
			}
		}
		for i, c := range covered {
			require.Equal(t, 1, c, "n=%d position %d", n, i)
		}
	}
}

// TestRouteSampleSpansBlock requires the route of a full-size block to follow the dependencies of the whole
// block rather than of its first occDependencySample transactions.
func TestRouteSampleSpansBlock(t *testing.T) {
	want := map[string]occPath{
		// 256 of 2,000 transactions chain; the block holds too few such reads to leave the frontier.
		"hot_prefix": occPathFrontier,
		// The first 256 transactions are independent and the 1,744 after them chain through one slot.
		"hot_tail": occPathSequential,
		// Each of 256 slots is incremented by about eight transactions spread through the block.
		"hot_slots_256": occPathSequential,
		"independent":   occPathFrontier,
	}
	for _, shape := range routeShapes() {
		path, ok := want[shape.name]
		if !ok {
			continue
		}
		t.Run(shape.name, func(t *testing.T) {
			req, state := buildRouteBlock(t, shape, routeBlockTxs)
			results := speculate(t, req, state)
			require.Equal(t, path, routeAfterSpeculation(occSampledResults(results, occDependencySpans(len(results)))))
			executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 8})
			defer executor.Close()
			got, err := executor.executePreparedBlock(t.Context(), req, state)
			require.NoError(t, err)
			require.Equal(t, path == occPathSequential, got.OCCStats.FallbackReason == occFallbackReasonDependent)
		})
	}
}

// TestOCCRouteMatchesSequential runs every route shape on each path after speculation and on the chosen one,
// and requires each result to equal the sequential path's.
func TestOCCRouteMatchesSequential(t *testing.T) {
	for _, shape := range routeShapes() {
		t.Run(shape.name, func(t *testing.T) {
			req, state := buildRouteBlock(t, shape, 300)
			want, err := NewExecutor(Config{MinGasPrice: big.NewInt(0)}).executePreparedBlock(t.Context(), req, state)
			require.NoError(t, err)
			for _, path := range []occPath{occPathAuto, occPathFrontier, occPathSequential} {
				executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 8})
				executor.occPath = path
				got, err := executor.executePreparedBlock(t.Context(), req, state)
				require.NoError(t, err)
				require.Equal(t, want.GasUsed, got.GasUsed)
				require.Equal(t, want.Txs, got.Txs)
				require.Equal(t, want.Receipts, got.Receipts)
				require.Equal(t, want.ChangeSet, got.ChangeSet)
				executor.Close()
			}
		})
	}
}

// TestMeasureDependenciesCoinbaseFeeCredits requires a fee credit to the coinbase to chain a later read of the
// coinbase balance, and to chain nothing between transactions that only pay fees.
func TestMeasureDependenciesCoinbaseFeeCredits(t *testing.T) {
	const n = occDependencySample
	chainID := big.NewInt(testChainID)
	coinbase := blockContext(chainID).Coinbase
	// SSTORE(CALLER, BALANCE(coinbase)): each caller stores the coinbase balance in its own slot.
	reader := testAddress(0xa3)
	readerCode := append(append([]byte{0x73}, coinbase.Bytes()...), 0x31, 0x33, 0x55, 0x00)
	for _, tc := range []struct {
		name string
		to   common.Address
		want occDependencies
		path occPath
	}{
		{"reads_coinbase", reader, occDependencies{dependent: n - 1, depth: n}, occPathSequential},
		{"never_reads_coinbase", routeSenderCounter, occDependencies{dependent: 0, depth: 1}, occPathFrontier},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shape := routeShape{tc.name, func(n int) []routeTx {
				txs := make([]routeTx, n)
				for i := range txs {
					txs[i] = routeTx{sender: i, to: tc.to}
				}
				return txs
			}}
			req, state := buildRouteBlock(t, shape, n)
			state.SetCode(reader, readerCode)
			results := speculate(t, req, state)
			for i := range results {
				require.Positive(t, results[i].commutativeBalanceDeltas[coinbase].Sign(), "tx %d pays no fee", i)
			}
			require.Equal(t, tc.want, measureDependencies(results))
			require.Equal(t, tc.path, routeAfterSpeculation(occSampledResults(results, occDependencySpans(len(results)))))
		})
	}
}
