package receipt

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func TestLogBudgetCountLimit(t *testing.T) {
	t.Parallel()
	budget := NewLogBudget(3, 0)
	log := &ethtypes.Log{Address: common.HexToAddress("0x1")}

	require.NoError(t, budget.Reserve(log))
	require.NoError(t, budget.Reserve(log))
	require.NoError(t, budget.Reserve(log))
	require.ErrorIs(t, budget.Reserve(log), ErrTooManyLogs)
	require.True(t, budget.Tripped())
}

func TestLogBudgetByteLimitOffByOne(t *testing.T) {
	t.Parallel()
	singleLogBytes := EstimateLogHeapBytes(&ethtypes.Log{
		Address: common.HexToAddress("0x1"),
		Data:    []byte("x"),
	})
	budget := NewLogBudget(0, singleLogBytes)

	log := &ethtypes.Log{
		Address: common.HexToAddress("0x1"),
		Data:    []byte("x"),
	}
	require.NoError(t, budget.Reserve(log))
	require.ErrorIs(t, budget.Reserve(log), ErrTooManyLogBytes)
}

func TestLogBudgetHugeDataUnderCountCap(t *testing.T) {
	t.Parallel()
	hugeData := make([]byte, 200<<10)
	log := &ethtypes.Log{
		Address: common.HexToAddress("0x1"),
		Data:    hugeData,
	}
	budget := NewLogBudget(100, EstimateLogHeapBytes(log)-1)

	require.ErrorIs(t, budget.Reserve(log), ErrTooManyLogBytes)
	require.True(t, budget.Tripped())
}

func TestLogBudgetConcurrentSlack(t *testing.T) {
	t.Parallel()
	const (
		workers = 8
		maxLog  = int64(100)
	)
	budget := NewLogBudget(maxLog, 0)
	log := &ethtypes.Log{Address: common.HexToAddress("0x1")}

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = budget.Reserve(log)
			}
		}()
	}
	wg.Wait()

	require.True(t, budget.Tripped())
	// Reserve is mutex-protected, so count should not exceed maxLog by more than
	// the single in-flight reservation semantics allow (exactly maxLog reserved).
	require.LessOrEqual(t, budget.usedCount, maxLog)
}

func TestLogBudgetConcurrentAtomicSlackDocumented(t *testing.T) {
	t.Parallel()
	// Document that Tripped() may be observed slightly after concurrent workers
	// stop appending; usedCount remains bounded by Reserve's mutex.
	budget := NewLogBudget(1, 0)
	log := &ethtypes.Log{Address: common.HexToAddress("0x1")}

	var started int32
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			atomic.AddInt32(&started, 1)
			for atomic.LoadInt32(&started) < 2 {
			}
			_ = budget.Reserve(log)
		}()
	}
	wg.Wait()
	require.True(t, budget.Tripped())
	require.Equal(t, int64(1), budget.usedCount)
}
