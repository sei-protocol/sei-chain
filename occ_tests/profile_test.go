package occ

import (
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/occ_tests/messages"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/stretchr/testify/require"
)

// TestPerfEvmTransferWithProfiling runs the test with CPU, memory, and execution trace profiling
func TestPerfEvmTransferWithProfiling(t *testing.T) {
	// CPU Profile
	cpuFile, err := os.Create("cpu.prof")
	require.NoError(t, err)
	defer cpuFile.Close()
	pprof.StartCPUProfile(cpuFile)
	defer pprof.StopCPUProfile()

	// Memory Profile (will be written at the end)
	defer func() {
		memFile, err := os.Create("mem.prof")
		require.NoError(t, err)
		defer memFile.Close()
		runtime.GC() // get up-to-date statistics
		pprof.WriteHeapProfile(memFile)
	}()

	// Execution Trace (shows goroutine scheduling, GC, etc.)
	traceFile, err := os.Create("trace.out")
	require.NoError(t, err)
	defer traceFile.Close()
	trace.Start(traceFile)
	defer trace.Stop()

	// Goroutine Profile
	defer func() {
		goroutineFile, err := os.Create("goroutine.prof")
		require.NoError(t, err)
		defer goroutineFile.Close()
		pprof.Lookup("goroutine").WriteTo(goroutineFile, 0)
	}()

	// Run the actual test
	blockTime := time.Now()
	accts := utils.NewTestAccounts(500)
	ctx := utils.NewTestContextWithTracing(t, accts, blockTime, 500, true)
	txs := utils.JoinMsgs(messages.EVMTransferNonConflicting(ctx, 500))

	_, pResults, _, duration, pErr := utils.RunWithOCC(ctx, txs)
	require.NoError(t, pErr)
	require.Len(t, pResults, len(txs))
	t.Logf("duration = %v", duration)
	t.Logf("Profiles written to: cpu.prof, mem.prof, trace.out, goroutine.prof")
}
