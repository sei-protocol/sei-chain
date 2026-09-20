package evmonly

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseSizerUsesEveryWorkerUntilItHasAnEstimate(t *testing.T) {
	sizer := newParseSizer(8)
	require.Equal(t, 8, sizer.workers(1000, time.Millisecond))
	require.Equal(t, 5, sizer.workers(5, time.Millisecond))
	require.Equal(t, 1, sizer.workers(1, time.Millisecond))
	require.Equal(t, 1, sizer.workers(0, time.Millisecond))
}

func TestParseSizerFitsTheEstimatedCostInTheBudget(t *testing.T) {
	sizer := newParseSizer(32)
	// 1000 txs on 4 workers in 12.5ms: 50µs per tx.
	sizer.observe(1000, 4, 12500*time.Microsecond)

	// 1000 txs is 50ms of work: 5 workers fit it in 10ms, 3 in 20ms, 1 in 50ms or more.
	require.Equal(t, 5, sizer.workers(1000, 10*time.Millisecond))
	require.Equal(t, 3, sizer.workers(1000, 20*time.Millisecond))
	require.Equal(t, 1, sizer.workers(1000, 50*time.Millisecond))
	require.Equal(t, 1, sizer.workers(1000, time.Second))
	// A block too large for the budget gets every worker, but never more than it has txs.
	require.Equal(t, 32, sizer.workers(5000, time.Millisecond))
	require.Equal(t, 10, sizer.workers(10, time.Microsecond))
}

func TestParseSizerWithoutABudgetUsesEveryWorker(t *testing.T) {
	sizer := newParseSizer(8)
	sizer.observe(1000, 8, time.Millisecond)
	require.Equal(t, 8, sizer.workers(1000, 0))
	require.Equal(t, 8, sizer.workers(1000, -time.Second))
}

func TestParseSizerFollowsTheMeasuredCost(t *testing.T) {
	sizer := newParseSizer(64)
	sizer.observe(1000, 1, 10*time.Millisecond)
	require.Equal(t, int64(10*time.Microsecond), sizer.perTx.Load())
	// The estimate moves a step towards each new measurement rather than jumping.
	sizer.observe(1000, 1, 90*time.Millisecond)
	require.Equal(t, int64(20*time.Microsecond), sizer.perTx.Load())
	// Zero-sized observations are ignored.
	sizer.observe(0, 1, time.Millisecond)
	sizer.observe(1000, 0, time.Millisecond)
	sizer.observe(1000, 1, 0)
	require.Equal(t, int64(20*time.Microsecond), sizer.perTx.Load())
}
