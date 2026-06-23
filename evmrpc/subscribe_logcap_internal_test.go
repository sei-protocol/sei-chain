package evmrpc

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// newLogCapAPI builds a minimal SubscriptionAPI exercising only the logs
// subscription cap helpers (acquireLogSub/releaseLogSub).
func newLogCapAPI(limit uint64) *SubscriptionAPI {
	return &SubscriptionAPI{
		subscriptonConfig: &SubscriptionConfig{logLimit: limit},
	}
}

func TestLogSubCapAcquireRelease(t *testing.T) {
	a := newLogCapAPI(2)

	// Acquire up to the limit.
	require.True(t, a.acquireLogSub())
	require.True(t, a.acquireLogSub())
	// Next acquisition is rejected once the cap is reached.
	require.False(t, a.acquireLogSub())

	// Releasing a slot lets a new subscription in again.
	a.releaseLogSub()
	require.True(t, a.acquireLogSub())
	require.False(t, a.acquireLogSub())
}

// TestLogSubCapZeroRejectsAll mirrors NewHeads' >= semantics: a limit of 0
// rejects every subscription.
func TestLogSubCapZeroRejectsAll(t *testing.T) {
	a := newLogCapAPI(0)
	require.False(t, a.acquireLogSub())
}

// TestLogSubReleaseNeverUnderflows guards the counter against dropping below
// zero if release is somehow called more than acquire.
func TestLogSubReleaseNeverUnderflows(t *testing.T) {
	a := newLogCapAPI(1)
	a.releaseLogSub() // count already 0; must stay at 0, not wrap around
	require.Equal(t, uint64(0), a.logSubsCount.Load())
	require.True(t, a.acquireLogSub())
}

// TestLogSubCapConcurrent proves the compare-and-swap operation never admits more
// than the limit even when many goroutines race for the last slots: Run with -race.
func TestLogSubCapConcurrent(t *testing.T) {
	const limit = 50
	const goroutines = 500
	a := newLogCapAPI(limit)

	var granted atomic.Int64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			if a.acquireLogSub() {
				granted.Add(1)
			}
		}()
	}
	wg.Wait()

	// Exactly `limit` acquisitions succeed; the count reflects them and never
	// overshoots the cap.
	require.Equal(t, int64(limit), granted.Load())
	require.Equal(t, uint64(limit), a.logSubsCount.Load())
}
