package disktable

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/Layr-Labs/eigenda/common"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/stretchr/testify/require"
)

// Note from the author (cody.littley): it's really tricky to validate rate limiting behavior without writing tests
// that rely on timing, to some extent. If these test flake, let me know, and we can either loosen
// the timing requirements or disable them.

// Flush 1000 times in a second, but limit actual flush rate to 10 times a second.
func TestRapidFlushes(t *testing.T) {
	// This test is inherently timing sensitive, don't parallelize it.

	logger, err := common.NewLogger(common.DefaultLoggerConfig())
	require.NoError(t, err)

	errorMonitor := util.NewErrorMonitor(t.Context(), logger, nil)

	flushCount := atomic.Uint64{}
	flushFunction := func() error {
		flushCount.Add(1)
		return nil
	}

	desiredFlushPeriod := 100 * time.Millisecond
	encounteredFlushPeriod := time.Millisecond

	fc := newFlushCoordinator(errorMonitor, flushFunction, desiredFlushPeriod)

	completionChan := make(chan struct{})

	// Send a bunch of rapid flush requests on background goroutines.
	ticker := time.NewTicker(encounteredFlushPeriod)
	defer ticker.Stop()
	for i := 0; i < 1000; i++ {
		<-ticker.C
		go func() {
			err := fc.Flush()
			require.NoError(t, err)
			completionChan <- struct{}{}
		}()
		require.NoError(t, err)
	}

	// Wait for all flushes to unblock and complete.
	timer := time.NewTimer(2 * time.Second)
	for i := 0; i < 1000; i++ {
		select {
		case <-completionChan:
		case <-timer.C:
			require.Fail(t, "Timed out waiting for flushes to complete")
		}
	}

	// We should expect to see 11 flushes (one at t=0, then once per 100ms for the remaining second).
	// But assert for weaker conditions to avoid test flakiness.
	lowerBound := 5
	upperBound := 25
	require.True(t, flushCount.Load() >= uint64(lowerBound),
		"Expected at least %d flushes, got %d", lowerBound, flushCount.Load())
	require.True(t, flushCount.Load() <= uint64(upperBound),
		"Expected at most %d flushes, got %d", upperBound, flushCount.Load())

	ok, _ := errorMonitor.IsOk()
	require.True(t, ok)
	errorMonitor.Shutdown()
}

// If we flush slower than the maximum rate, then we should never wait that long for a flush.
func TestInfrequentFlushes(t *testing.T) {
	// This test is inherently timing sensitive, don't parallelize it.

	logger, err := common.NewLogger(common.DefaultLoggerConfig())
	require.NoError(t, err)

	errorMonitor := util.NewErrorMonitor(t.Context(), logger, nil)

	flushCount := atomic.Uint64{}
	flushFunction := func() error {
		flushCount.Add(1)
		return nil
	}

	desiredFlushPeriod := 100 * time.Millisecond

	fc := newFlushCoordinator(errorMonitor, flushFunction, desiredFlushPeriod)

	// The time to flush when unblocked is likely to be less than a millisecond, but only assert
	// that it is less than this value to avoid test flakiness.
	minimumFlushTime := desiredFlushPeriod / 2

	// The first flush should be very fast, since we can't be in violation of the rate limit at t=0.
	startTime := time.Now()
	err = fc.Flush()
	require.NoError(t, err)
	duration := time.Since(startTime)
	require.True(t, duration < minimumFlushTime,
		"Expected first flush to take less than %v, took %v", minimumFlushTime, duration)
	require.Equal(t, uint64(1), flushCount.Load())

	// The second flush should be delayed.
	startTime = time.Now()
	err = fc.Flush()
	require.NoError(t, err)
	duration = time.Since(startTime)
	require.True(t, duration >= minimumFlushTime,
		"Expected second flush to take at least %v, took %v", minimumFlushTime, duration)
	require.Equal(t, uint64(2), flushCount.Load())

	// Wait for 2x the flush period. The next flush should be able to happen immediately.
	time.Sleep(2 * desiredFlushPeriod)

	startTime = time.Now()
	err = fc.Flush()
	require.NoError(t, err)
	duration = time.Since(startTime)
	require.True(t, duration < minimumFlushTime,
		"Expected third flush to take less than %v, took %v", minimumFlushTime, duration)
	require.Equal(t, uint64(3), flushCount.Load())

	ok, _ := errorMonitor.IsOk()
	require.True(t, ok)
	errorMonitor.Shutdown()
}
