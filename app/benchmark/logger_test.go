package benchmark

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
)

func TestCalculateTPS(t *testing.T) {
	tests := []struct {
		name     string
		txCount  int64
		duration time.Duration
		expected float64
	}{
		{
			name:     "normal case - 1000 txs in 1 second",
			txCount:  1000,
			duration: 1 * time.Second,
			expected: 1000.0,
		},
		{
			name:     "normal case - 5000 txs in 5 seconds",
			txCount:  5000,
			duration: 5 * time.Second,
			expected: 1000.0,
		},
		{
			name:     "zero duration",
			txCount:  1000,
			duration: 0,
			expected: 0.0,
		},
		{
			name:     "negative duration",
			txCount:  1000,
			duration: -1 * time.Second,
			expected: 0.0,
		},
		{
			name:     "zero transactions",
			txCount:  0,
			duration: 5 * time.Second,
			expected: 0.0,
		},
		{
			name:     "fractional seconds",
			txCount:  1000,
			duration: 2 * time.Second,
			expected: 500.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateTPS(tt.txCount, tt.duration)
			require.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestCalculateAvgBlockTime(t *testing.T) {
	tests := []struct {
		name           string
		totalBlockTime time.Duration
		blockTimeCount int64
		expected       int64
	}{
		{
			name:           "normal case - 5 blocks with 1000ms total",
			totalBlockTime: 5000 * time.Millisecond,
			blockTimeCount: 5,
			expected:       1000,
		},
		{
			name:           "normal case - 10 blocks with 2000ms total",
			totalBlockTime: 20000 * time.Millisecond,
			blockTimeCount: 10,
			expected:       2000,
		},
		{
			name:           "zero count",
			totalBlockTime: 5000 * time.Millisecond,
			blockTimeCount: 0,
			expected:       0,
		},
		{
			name:           "zero total time",
			totalBlockTime: 0,
			blockTimeCount: 5,
			expected:       0,
		},
		{
			name:           "fractional milliseconds",
			totalBlockTime: 3333 * time.Millisecond,
			blockTimeCount: 3,
			expected:       1111, // 3333/3 = 1111ms
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateAvgBlockTime(tt.totalBlockTime, tt.blockTimeCount)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateTheoreticalTPS(t *testing.T) {
	tests := []struct {
		name              string
		txCount           int64
		blockCount        int64
		avgBlockProcessMs int64
		expected          float64
	}{
		{
			name:              "10k txs in 10 blocks, 500ms avg process time",
			txCount:           10000,
			blockCount:        10,
			avgBlockProcessMs: 500,
			expected:          2000.0, // 1000 txs/block * (1000/500) = 2000 TPS
		},
		{
			name:              "1k txs in 1 block, 100ms process time",
			txCount:           1000,
			blockCount:        1,
			avgBlockProcessMs: 100,
			expected:          10000.0, // 1000 * (1000/100) = 10000 TPS
		},
		{
			name:              "zero block count",
			txCount:           1000,
			blockCount:        0,
			avgBlockProcessMs: 500,
			expected:          0.0,
		},
		{
			name:              "zero process time",
			txCount:           1000,
			blockCount:        10,
			avgBlockProcessMs: 0,
			expected:          0.0,
		},
		{
			name:              "zero transactions",
			txCount:           0,
			blockCount:        10,
			avgBlockProcessMs: 500,
			expected:          0.0,
		},
		{
			name:              "negative block count",
			txCount:           1000,
			blockCount:        -1,
			avgBlockProcessMs: 500,
			expected:          0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateTheoreticalTPS(tt.txCount, tt.blockCount, tt.avgBlockProcessMs)
			require.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestLogger_Increment(t *testing.T) {
	bl := NewLogger(log.NewNopLogger())

	baseTime := time.Now()

	// First increment should initialize lastFlushTime
	bl.Increment(100, baseTime, 1)
	require.False(t, bl.lastFlushTime.IsZero())
	require.Equal(t, int64(100), bl.txCount)
	require.Equal(t, int64(1), bl.blockCount)
	require.Equal(t, int64(1), bl.latestHeight)

	// Second increment should update maxBlockTime if larger
	time2 := baseTime.Add(2 * time.Second)
	bl.Increment(200, time2, 2)
	require.Equal(t, int64(300), bl.txCount)
	require.Equal(t, int64(2), bl.blockCount)
	require.Equal(t, int64(2), bl.latestHeight)
	require.Equal(t, 2*time.Second, bl.maxBlockTime)
	require.Equal(t, int64(1), bl.blockTimeCount)

	// Third increment with smaller time diff should not update maxBlockTime
	time3 := time2.Add(500 * time.Millisecond)
	bl.Increment(150, time3, 3)
	require.Equal(t, int64(450), bl.txCount)
	require.Equal(t, int64(3), bl.blockCount)
	require.Equal(t, int64(3), bl.latestHeight)
	require.Equal(t, 2*time.Second, bl.maxBlockTime) // Still the max
	require.Equal(t, int64(2), bl.blockTimeCount)

	// Increment with higher height should update latestHeight
	bl.Increment(100, time3, 5)
	require.Equal(t, int64(5), bl.latestHeight)
}

func TestLogger_GetAndResetStats(t *testing.T) {
	bl := NewLogger(log.NewNopLogger())

	baseTime := time.Now()
	bl.lastFlushTime = baseTime

	// Set up some stats
	bl.txCount = 1000
	bl.blockCount = 10
	bl.latestHeight = 100
	bl.maxBlockTime = 2 * time.Second
	bl.totalBlockTime = 10 * time.Second
	bl.blockTimeCount = 9 // 9 intervals between 10 blocks

	// Wait a bit to ensure duration > 0
	time.Sleep(10 * time.Millisecond)
	now := time.Now()

	stats, prevTime := bl.getAndResetStats(now)

	// Check stats were captured correctly
	require.Equal(t, int64(1000), stats.txCount)
	require.Equal(t, int64(10), stats.blockCount)
	require.Equal(t, int64(100), stats.latestHeight)
	require.Equal(t, int64(2000), stats.maxBlockTimeMs)
	require.InDelta(t, 1111, stats.avgBlockTimeMs, 1) // 10000ms / 9 ≈ 1111ms
	require.Equal(t, baseTime, prevTime)

	// Check TPS calculation
	duration := now.Sub(baseTime)
	expectedTPS := calculateTPS(1000, duration)
	require.InDelta(t, expectedTPS, stats.tps, 0.01)

	// Check counters were reset
	require.Equal(t, int64(0), bl.txCount)
	require.Equal(t, int64(0), bl.blockCount)
	require.Equal(t, int64(0), bl.latestHeight)
	require.Equal(t, time.Duration(0), bl.maxBlockTime)
	require.Equal(t, time.Duration(0), bl.totalBlockTime)
	require.Equal(t, int64(0), bl.blockTimeCount)
	require.Equal(t, now, bl.lastFlushTime)
}

func TestLogger_StartStop(t *testing.T) {
	bl := NewLogger(log.NewNopLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the logger
	done := make(chan bool)
	go func() {
		bl.Start(ctx)
		done <- true
	}()

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Add some increments
	baseTime := time.Now()
	bl.Increment(100, baseTime, 1)
	bl.Increment(200, baseTime.Add(time.Second), 2)

	// Wait for at least one flush (should happen after 5 seconds, but we'll cancel earlier)
	time.Sleep(100 * time.Millisecond)

	// Cancel context to stop the logger
	cancel()

	// Wait for goroutine to finish
	select {
	case <-done:
		// Successfully stopped
	case <-time.After(1 * time.Second):
		t.Fatal("Logger did not stop within timeout")
	}
}

func TestLogger_ConcurrentIncrement(t *testing.T) {
	bl := NewLogger(log.NewNopLogger())

	baseTime := time.Now()
	numGoroutines := 10
	iterationsPerGoroutine := 100

	// Run concurrent increments
	done := make(chan bool, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < iterationsPerGoroutine; j++ {
				bl.Increment(1, baseTime.Add(time.Duration(id*iterationsPerGoroutine+j)*time.Millisecond), int64(id*iterationsPerGoroutine+j))
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify final counts
	require.Equal(t, int64(numGoroutines*iterationsPerGoroutine), bl.txCount)
	require.Equal(t, int64(numGoroutines*iterationsPerGoroutine), bl.blockCount)
}

func TestLogger_BlockTimeCalculations(t *testing.T) {
	bl := NewLogger(log.NewNopLogger())

	baseTime := time.Now()

	// Increment with increasing block times
	bl.Increment(100, baseTime, 1)
	bl.Increment(100, baseTime.Add(100*time.Millisecond), 2) // 100ms diff
	bl.Increment(100, baseTime.Add(250*time.Millisecond), 3) // 150ms diff
	bl.Increment(100, baseTime.Add(600*time.Millisecond), 4) // 350ms diff (max)

	require.Equal(t, 350*time.Millisecond, bl.maxBlockTime)
	require.Equal(t, int64(3), bl.blockTimeCount)
	require.Equal(t, 600*time.Millisecond, bl.totalBlockTime) // 100 + 150 + 350

	// Flush and verify stats
	bl.lastFlushTime = baseTime
	time.Sleep(10 * time.Millisecond)
	stats, _ := bl.getAndResetStats(time.Now())
	require.Equal(t, int64(350), stats.maxBlockTimeMs)
	require.Equal(t, int64(200), stats.avgBlockTimeMs) // 600ms / 3 = 200ms
}

func TestLogger_FlushLog(t *testing.T) {
	bl := NewLogger(log.NewNopLogger())

	baseTime := time.Now()
	bl.lastFlushTime = baseTime

	// Set up some stats
	bl.txCount = 5000
	bl.blockCount = 10
	bl.latestHeight = 100
	bl.maxBlockTime = 2 * time.Second
	bl.totalBlockTime = 10 * time.Second
	bl.blockTimeCount = 9

	// Wait a bit to ensure duration > 0
	time.Sleep(10 * time.Millisecond)

	// Call FlushLog - this should not panic and should reset stats
	bl.FlushLog()

	// Verify stats were reset after flush
	require.Equal(t, int64(0), bl.txCount)
	require.Equal(t, int64(0), bl.blockCount)
	require.Equal(t, int64(0), bl.latestHeight)
	require.Equal(t, time.Duration(0), bl.maxBlockTime)
	require.Equal(t, time.Duration(0), bl.totalBlockTime)
	require.Equal(t, int64(0), bl.blockTimeCount)
	require.False(t, bl.lastFlushTime.IsZero(), "lastFlushTime should be updated")

	// Test FlushLog with zero stats (should not panic)
	bl.FlushLog()
	require.Equal(t, int64(0), bl.txCount)
}

func TestLogger_RecordCommitTime(t *testing.T) {
	bl := NewLogger(log.NewNopLogger())

	// Record some commit times
	bl.RecordCommitTime(100 * time.Millisecond)
	bl.RecordCommitTime(200 * time.Millisecond)
	bl.RecordCommitTime(150 * time.Millisecond)

	require.Equal(t, 200*time.Millisecond, bl.maxCommitTime)
	require.Equal(t, 450*time.Millisecond, bl.totalCommitTime)
	require.Equal(t, int64(3), bl.commitCount)
}

func TestLogger_BlockProcessingTime(t *testing.T) {
	bl := NewLogger(log.NewNopLogger())

	// Start and end block processing
	bl.StartBlockProcessing()
	time.Sleep(50 * time.Millisecond)
	bl.EndBlockProcessing()

	require.GreaterOrEqual(t, bl.maxBlockProcessTime, 50*time.Millisecond)
	require.Equal(t, int64(1), bl.blockProcessCount)

	// Second block processing
	bl.StartBlockProcessing()
	time.Sleep(100 * time.Millisecond)
	bl.EndBlockProcessing()

	require.GreaterOrEqual(t, bl.maxBlockProcessTime, 100*time.Millisecond)
	require.Equal(t, int64(2), bl.blockProcessCount)
}
