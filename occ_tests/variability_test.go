package occ

import (
	"math"
	"runtime"
	"runtime/debug"
	"sort"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/occ_tests/messages"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/stretchr/testify/require"
)

// TestVariability_MultipleRuns measures consistency across multiple runs
func TestVariability_MultipleRuns(t *testing.T) {
	t.Log("=== Variability Analysis: Multiple Runs ===")
	t.Log("Purpose: Measure consistency and identify sources of variance")

	numRuns := 10
	workerCounts := []int{100, 200, 500}

	for _, workers := range workerCounts {
		t.Logf("\n--- Testing %d workers with %d runs ---", workers, numRuns)

		durations := make([]time.Duration, numRuns)

		for i := 0; i < numRuns; i++ {
			// Force GC before each run for consistency
			runtime.GC()
			time.Sleep(10 * time.Millisecond) // Let system settle

			blockTime := time.Now()
			accts := utils.NewTestAccounts(500)
			ctx := utils.NewTestContext(t, accts, blockTime, workers, true)
			txs := utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))

			start := time.Now()
			_, pResults, _, _, pErr := utils.RunWithOCC(ctx, txs)
			elapsed := time.Since(start)

			require.NoError(t, pErr)
			require.Len(t, pResults, len(txs))

			durations[i] = elapsed
			t.Logf("  Run %2d: %v", i+1, elapsed)
		}

		// Calculate statistics
		mean, stddev, min, max, cv := calculateStats(durations)

		t.Logf("\n  Statistics for %d workers:", workers)
		t.Logf("    Mean:   %v", mean)
		t.Logf("    StdDev: %v (%.1f%%)", stddev, cv*100)
		t.Logf("    Min:    %v", min)
		t.Logf("    Max:    %v", max)
		t.Logf("    Range:  %v (%.1fx)", max-min, float64(max)/float64(min))

		if cv > 0.1 {
			t.Logf("  ⚠️  High variability (CV=%.1f%%) - inconsistent performance", cv*100)
		} else {
			t.Logf("  ✓ Low variability (CV=%.1f%%) - consistent performance", cv*100)
		}
	}
}

// TestVariability_CacheEffects tests if cache warming affects performance
func TestVariability_CacheEffects(t *testing.T) {
	t.Log("=== Variability Analysis: Cache Effects ===")
	t.Log("Purpose: Test if cache warming improves performance")

	workers := 200

	// Cold run (first execution)
	t.Log("\n--- Cold Run (no cache warming) ---")
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	blockTime := time.Now()
	accts := utils.NewTestAccounts(500)
	ctx := utils.NewTestContext(t, accts, blockTime, workers, true)
	txs := utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))

	start := time.Now()
	_, pResults, _, _, pErr := utils.RunWithOCC(ctx, txs)
	coldTime := time.Since(start)

	require.NoError(t, pErr)
	require.Len(t, pResults, len(txs))
	t.Logf("Cold run: %v", coldTime)

	// Warm runs (cache should be hot)
	t.Log("\n--- Warm Runs (cache warmed up) ---")
	warmTimes := make([]time.Duration, 5)

	for i := 0; i < 5; i++ {
		// Don't GC, keep cache warm
		blockTime = time.Now()
		accts = utils.NewTestAccounts(500)
		ctx = utils.NewTestContext(t, accts, blockTime, workers, true)
		txs = utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))

		start = time.Now()
		_, pResults, _, _, pErr = utils.RunWithOCC(ctx, txs)
		warmTimes[i] = time.Since(start)

		require.NoError(t, pErr)
		require.Len(t, pResults, len(txs))
		t.Logf("  Warm run %d: %v", i+1, warmTimes[i])
	}

	warmMean, _, _, _, _ := calculateStats(warmTimes)
	improvement := float64(coldTime-warmMean) / float64(coldTime) * 100

	t.Logf("\nCold vs Warm:")
	t.Logf("  Cold: %v", coldTime)
	t.Logf("  Warm: %v (avg)", warmMean)
	t.Logf("  Improvement: %.1f%%", improvement)

	if improvement > 10 {
		t.Log("  ⚠️  Significant cache effect - cold starts are much slower")
	} else {
		t.Log("  ✓ Minimal cache effect - performance is consistent")
	}
}

// TestVariability_SystemLoad tests if system load affects performance
func TestVariability_SystemLoad(t *testing.T) {
	t.Log("=== Variability Analysis: System Load ===")
	t.Log("Purpose: Test if CPU contention affects performance")

	workers := 200

	// Baseline (no extra load)
	t.Log("\n--- Baseline (no extra load) ---")
	runtime.GC()

	blockTime := time.Now()
	accts := utils.NewTestAccounts(500)
	ctx := utils.NewTestContext(t, accts, blockTime, workers, true)
	txs := utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))

	start := time.Now()
	_, pResults, _, _, pErr := utils.RunWithOCC(ctx, txs)
	baselineTime := time.Since(start)

	require.NoError(t, pErr)
	require.Len(t, pResults, len(txs))
	t.Logf("Baseline: %v", baselineTime)

	// With CPU load (spin up background goroutines)
	t.Log("\n--- With Background CPU Load ---")

	// Create CPU load
	stopLoad := make(chan struct{})
	numCPU := runtime.NumCPU()
	for i := 0; i < numCPU/2; i++ { // Use half the CPUs for load
		go func() {
			x := 0
			for {
				select {
				case <-stopLoad:
					return
				default:
					x++ // Busy loop
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond) // Let load stabilize

	blockTime = time.Now()
	accts = utils.NewTestAccounts(500)
	ctx = utils.NewTestContext(t, accts, blockTime, workers, true)
	txs = utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))

	start = time.Now()
	_, pResults, _, _, pErr = utils.RunWithOCC(ctx, txs)
	loadedTime := time.Since(start)

	close(stopLoad) // Stop background load

	require.NoError(t, pErr)
	require.Len(t, pResults, len(txs))
	t.Logf("With load: %v", loadedTime)

	slowdown := float64(loadedTime-baselineTime) / float64(baselineTime) * 100

	t.Logf("\nBaseline vs Loaded:")
	t.Logf("  Baseline: %v", baselineTime)
	t.Logf("  Loaded:   %v", loadedTime)
	t.Logf("  Slowdown: %.1f%%", slowdown)

	if slowdown > 20 {
		t.Log("  ⚠️  Sensitive to CPU contention - system load matters")
	} else {
		t.Log("  ✓ Resilient to CPU contention - system load doesn't matter much")
	}
}

// TestVariability_MemoryPressure tests if memory pressure affects performance
func TestVariability_MemoryPressure(t *testing.T) {
	t.Log("=== Variability Analysis: Memory Pressure ===")
	t.Log("Purpose: Test if heap size affects performance")

	workers := 200

	// Small heap (force frequent GC)
	t.Log("\n--- Small Heap (GOGC=20) ---")
	oldGOGC := runtime.GOMAXPROCS(0)
	debug.SetGCPercent(20) // Aggressive GC
	runtime.GC()

	blockTime := time.Now()
	accts := utils.NewTestAccounts(500)
	ctx := utils.NewTestContext(t, accts, blockTime, workers, true)
	txs := utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))

	start := time.Now()
	_, pResults, _, _, pErr := utils.RunWithOCC(ctx, txs)
	smallHeapTime := time.Since(start)

	require.NoError(t, pErr)
	require.Len(t, pResults, len(txs))
	t.Logf("Small heap: %v", smallHeapTime)

	// Large heap (infrequent GC)
	t.Log("\n--- Large Heap (GOGC=400) ---")
	debug.SetGCPercent(400) // Lazy GC
	runtime.GC()

	blockTime = time.Now()
	accts = utils.NewTestAccounts(500)
	ctx = utils.NewTestContext(t, accts, blockTime, workers, true)
	txs = utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))

	start = time.Now()
	_, pResults, _, _, pErr = utils.RunWithOCC(ctx, txs)
	largeHeapTime := time.Since(start)

	debug.SetGCPercent(oldGOGC) // Restore

	require.NoError(t, pErr)
	require.Len(t, pResults, len(txs))
	t.Logf("Large heap: %v", largeHeapTime)

	improvement := float64(smallHeapTime-largeHeapTime) / float64(smallHeapTime) * 100

	t.Logf("\nSmall vs Large Heap:")
	t.Logf("  Small (GOGC=20):  %v", smallHeapTime)
	t.Logf("  Large (GOGC=400): %v", largeHeapTime)
	t.Logf("  Improvement: %.1f%%", improvement)

	if improvement > 15 {
		t.Log("  ⚠️  GC frequency matters - tune GOGC for production")
	} else {
		t.Log("  ✓ GC frequency doesn't matter much")
	}
}

// Helper function to calculate statistics
func calculateStats(durations []time.Duration) (mean, stddev, min, max time.Duration, cv float64) {
	if len(durations) == 0 {
		return
	}

	// Convert to float64 for calculations
	values := make([]float64, len(durations))
	for i, d := range durations {
		values[i] = float64(d)
	}

	// Sort for min/max
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	min = sorted[0]
	max = sorted[len(sorted)-1]

	// Mean
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	meanFloat := sum / float64(len(values))
	mean = time.Duration(meanFloat)

	// Standard deviation
	variance := 0.0
	for _, v := range values {
		diff := v - meanFloat
		variance += diff * diff
	}
	variance /= float64(len(values))
	stddevFloat := math.Sqrt(variance)
	stddev = time.Duration(stddevFloat)

	// Coefficient of variation
	if meanFloat > 0 {
		cv = stddevFloat / meanFloat
	}

	return
}
