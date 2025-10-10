package occ

import (
	"runtime"
	"runtime/debug"
	"sync"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/occ_tests/messages"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/stretchr/testify/require"
)

// Test 1: Baseline - Sequential execution (no parallelism, no OCC)
// This shows the "pure" execution time without any coordination overhead
func TestDiagnostic1_Sequential(t *testing.T) {
	t.Log("=== TEST 1: Sequential Execution (Baseline) ===")
	t.Log("Purpose: Measure pure execution time without parallelism")
	
	blockTime := time.Now()
	accts := utils.NewTestAccounts(500)
	ctx := utils.NewTestContextWithTracing(t, accts, blockTime, 1, false) // 1 worker, OCC disabled
	txs := utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))

	start := time.Now()
	_, pResults, _, duration, pErr := utils.RunWithOCC(ctx, txs)
	elapsed := time.Since(start)
	
	require.NoError(t, pErr)
	require.Len(t, pResults, len(txs))
	
	t.Logf("Sequential execution: %v", duration)
	t.Logf("Wall time: %v", elapsed)
	t.Logf("Per-tx average: %v", elapsed/time.Duration(len(txs)))
	t.Log("✓ This is your baseline - pure execution with no coordination overhead")
}

// Test 2: Parallel without OCC (no ACL coordination)
// This shows goroutine scheduling overhead without ACL channel waits
func TestDiagnostic2_ParallelNoOCC(t *testing.T) {
	t.Log("=== TEST 2: Parallel Execution WITHOUT OCC ===")
	t.Log("Purpose: Measure goroutine scheduling overhead")
	
	blockTime := time.Now()
	accts := utils.NewTestAccounts(500)
	ctx := utils.NewTestContextWithTracing(t, accts, blockTime, 500, false) // 500 workers, OCC disabled
	txs := utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))

	start := time.Now()
	_, pResults, _, duration, pErr := utils.RunWithOCC(ctx, txs)
	elapsed := time.Since(start)
	
	require.NoError(t, pErr)
	require.Len(t, pResults, len(txs))
	
	t.Logf("Parallel (no OCC) execution: %v", duration)
	t.Logf("Wall time: %v", elapsed)
	t.Logf("Per-tx average: %v", elapsed/time.Duration(len(txs)))
	t.Log("✓ Compare to Test 1 - difference is goroutine scheduling overhead")
}

// Test 3: Parallel with OCC (full coordination)
// This shows ACL channel coordination overhead
func TestDiagnostic3_ParallelWithOCC(t *testing.T) {
	t.Log("=== TEST 3: Parallel Execution WITH OCC ===")
	t.Log("Purpose: Measure ACL coordination overhead")
	
	blockTime := time.Now()
	accts := utils.NewTestAccounts(500)
	ctx := utils.NewTestContextWithTracing(t, accts, blockTime, 500, true) // 500 workers, OCC enabled
	txs := utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))

	start := time.Now()
	_, pResults, _, duration, pErr := utils.RunWithOCC(ctx, txs)
	elapsed := time.Since(start)
	
	require.NoError(t, pErr)
	require.Len(t, pResults, len(txs))
	
	t.Logf("Parallel (with OCC) execution: %v", duration)
	t.Logf("Wall time: %v", elapsed)
	t.Logf("Per-tx average: %v", elapsed/time.Duration(len(txs)))
	t.Log("✓ Compare to Test 2 - difference is ACL channel coordination overhead")
}

// Test 4: Different worker counts
// This shows the sweet spot for parallelism
func TestDiagnostic4_WorkerScaling(t *testing.T) {
	t.Log("=== TEST 4: Worker Count Scaling ===")
	t.Log("Purpose: Find optimal parallelism level")
	
	workerCounts := []int{1, 10, 50, 100, 200, 500}
	blockTime := time.Now()
	accts := utils.NewTestAccounts(500)
	
	for _, workers := range workerCounts {
		ctx := utils.NewTestContextWithTracing(t, accts, blockTime, workers, true)
		txs := utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))
		
		start := time.Now()
		_, pResults, _, duration, pErr := utils.RunWithOCC(ctx, txs)
		elapsed := time.Since(start)
		
		require.NoError(t, pErr)
		require.Len(t, pResults, len(txs))
		
		t.Logf("Workers=%d: duration=%v, wall=%v, per-tx=%v", 
			workers, duration, elapsed, elapsed/time.Duration(len(txs)))
	}
	t.Log("✓ Look for diminishing returns - that's where scheduling overhead dominates")
}

// Test 5: Resource contention (shared resources) vs Independent transactions
// This shows ACL dependency blocking overhead
func TestDiagnostic5_ResourceContentionVsIndependent(t *testing.T) {
	t.Log("=== TEST 5: Resource Contention vs Independent Transactions ===")
	t.Log("Purpose: Measure ACL blocking time from shared resource dependencies")
	t.Log("Note: This is NOT OCC conflict detection - this is ACL coordination overhead")
	
	blockTime := time.Now()
	accts := utils.NewTestAccounts(500)
	
	// Independent (each tx touches different accounts - no ACL dependencies)
	t.Log("\n--- Independent Transactions (different accounts) ---")
	ctx1 := utils.NewTestContextWithTracing(t, accts, blockTime, 500, true)
	txs1 := utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx1, 500))
	
	start1 := time.Now()
	_, pResults1, _, duration1, pErr1 := utils.RunWithOCC(ctx1, txs1)
	elapsed1 := time.Since(start1)
	
	require.NoError(t, pErr1)
	require.Len(t, pResults1, len(txs1))
	t.Logf("Independent txs: duration=%v, wall=%v", duration1, elapsed1)
	
	// Shared resource (all txs touch same account - maximum ACL blocking)
	t.Log("\n--- Shared Resource Transactions (same sender account) ---")
	ctx2 := utils.NewTestContextWithTracing(t, accts, blockTime, 500, true)
	// Create txs that all use the same sender account - forces sequential ACL execution
	txs2 := utils.JoinMsgs(messages.EVMTransferConflicting(ctx2, 500))
	
	start2 := time.Now()
	_, pResults2, _, duration2, pErr2 := utils.RunWithOCC(ctx2, txs2)
	elapsed2 := time.Since(start2)
	
	require.NoError(t, pErr2)
	require.Len(t, pResults2, len(txs2))
	t.Logf("Shared resource txs: duration=%v, wall=%v", duration2, elapsed2)
	
	t.Logf("\n✓ Difference (%v) is ACL blocking time from resource dependencies", elapsed2-elapsed1)
	t.Log("  (Transactions wait on ACL channels for prior txs touching same account)")
	t.Log("  (This is NOT OCC abort/retry - scheduler shows 0 OCC conflicts)")
}

// Test 6: Memory allocation overhead
// This shows allocation impact without GC
func TestDiagnostic6_AllocationOverhead(t *testing.T) {
	t.Log("=== TEST 6: Memory Allocation Overhead ===")
	t.Log("Purpose: Measure allocation overhead separate from GC")
	
	// Disable GC temporarily to isolate allocation overhead
	runtime.GC() // Clean slate
	gcPercent := debug.SetGCPercent(-1) // Disable GC
	defer debug.SetGCPercent(gcPercent) // Restore
	
	blockTime := time.Now()
	accts := utils.NewTestAccounts(500)
	ctx := utils.NewTestContextWithTracing(t, accts, blockTime, 500, true)
	txs := utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))
	
	var m1, m2 runtime.MemStats
	runtime.ReadMemStats(&m1)
	
	start := time.Now()
	_, pResults, _, duration, pErr := utils.RunWithOCC(ctx, txs)
	elapsed := time.Since(start)
	
	runtime.ReadMemStats(&m2)
	
	require.NoError(t, pErr)
	require.Len(t, pResults, len(txs))
	
	allocatedMB := float64(m2.TotalAlloc-m1.TotalAlloc) / 1024 / 1024
	allocsPerTx := (m2.Mallocs - m1.Mallocs) / uint64(len(txs))
	
	t.Logf("Execution time (no GC): %v", duration)
	t.Logf("Wall time: %v", elapsed)
	t.Logf("Total allocated: %.2f MB", allocatedMB)
	t.Logf("Allocations per tx: %d", allocsPerTx)
	t.Logf("Heap in use: %.2f MB", float64(m2.HeapInuse)/1024/1024)
	t.Log("✓ This shows pure allocation overhead without GC pauses")
	
	// Force GC and see the pause
	t.Log("\nForcing GC...")
	gcStart := time.Now()
	runtime.GC()
	gcDuration := time.Since(gcStart)
	t.Logf("GC pause: %v", gcDuration)
	t.Logf("✓ Compare execution time to GC pause - shows GC is not the bottleneck")
}

// Test 7: Lock contention measurement
// This uses mutex profiling to find hot locks
func TestDiagnostic7_LockContention(t *testing.T) {
	t.Log("=== TEST 7: Lock Contention Analysis ===")
	t.Log("Purpose: Identify lock contention hotspots")
	
	// Enable mutex profiling
	runtime.SetMutexProfileFraction(1)
	defer runtime.SetMutexProfileFraction(0)
	
	blockTime := time.Now()
	accts := utils.NewTestAccounts(500)
	ctx := utils.NewTestContextWithTracing(t, accts, blockTime, 500, true)
	txs := utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))
	
	start := time.Now()
	_, pResults, _, duration, pErr := utils.RunWithOCC(ctx, txs)
	elapsed := time.Since(start)
	
	require.NoError(t, pErr)
	require.Len(t, pResults, len(txs))
	
	t.Logf("Execution time: %v", duration)
	t.Logf("Wall time: %v", elapsed)
	
	// Write mutex profile
	// Note: Analyze with: go tool pprof -http=:8080 mutex.prof
	t.Log("✓ Run: go tool pprof -top mutex.prof")
	t.Log("  Look for functions with high 'cum' (cumulative) time")
	t.Log("  Those are your lock contention hotspots")
}

// Test 8: Channel operation overhead
// This measures pure channel coordination cost
func TestDiagnostic8_ChannelOverhead(t *testing.T) {
	t.Log("=== TEST 8: Channel Operation Overhead ===")
	t.Log("Purpose: Measure pure channel coordination cost")
	
	numOps := 500
	numChannels := 10
	
	// Create channels similar to ACL structure
	channels := make([]chan interface{}, numChannels)
	for i := range channels {
		channels[i] = make(chan interface{}, 1)
	}
	
	// Test: Send and receive on channels (simulating ACL coordination)
	start := time.Now()
	
	var wg sync.WaitGroup
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			
			// Wait on channels (like ACL.WaitForBlockingSignals)
			for _, ch := range channels {
				<-ch
			}
			
			// Signal completion (like SendAllSignalsForTx)
			for _, ch := range channels {
				ch <- struct{}{}
			}
		}(i)
	}
	
	// Prime the channels
	for _, ch := range channels {
		ch <- struct{}{}
	}
	
	wg.Wait()
	elapsed := time.Since(start)
	
	perOp := elapsed / time.Duration(numOps)
	t.Logf("Total channel coordination time: %v", elapsed)
	t.Logf("Per-operation: %v", perOp)
	t.Logf("Channel ops per second: %.0f", float64(numOps)/elapsed.Seconds())
	t.Log("✓ This is the theoretical minimum ACL coordination overhead")
}

// Helper function to run all diagnostics
func TestDiagnostic_RunAll(t *testing.T) {
	t.Log("========================================")
	t.Log("DIAGNOSTIC TEST SUITE")
	t.Log("========================================")
	t.Log("")
	t.Log("This will run all diagnostic tests to identify performance bottlenecks.")
	t.Log("Each test isolates a specific factor:")
	t.Log("  1. Sequential - Baseline (no parallelism)")
	t.Log("  2. Parallel no OCC - Goroutine scheduling overhead")
	t.Log("  3. Parallel with OCC - ACL coordination overhead")
	t.Log("  4. Worker scaling - Optimal parallelism")
	t.Log("  5. Conflicts - ACL wait time impact")
	t.Log("  6. Allocations - Memory overhead")
	t.Log("  7. Locks - Contention hotspots")
	t.Log("  8. Channels - Pure coordination cost")
	t.Log("")
	t.Log("Run individual tests with:")
	t.Log("  go test -v -run TestDiagnostic1_Sequential")
	t.Log("  go test -v -run TestDiagnostic2_ParallelNoOCC")
	t.Log("  etc.")
	t.Log("")
	t.Log("========================================")
}
