package evmrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func admissionMetrics(pending int64, semInUse int32) *WorkerPoolMetrics {
	m := &WorkerPoolMetrics{}
	m.TasksSubmitted.Store(pending)
	m.QueueCapacity.Store(100)
	m.DBSemaphoreAcquired.Store(semInUse)
	m.DBSemaphoreCapacity.Store(20)
	return m
}

// TestLoadRefusalKeepsLargeQueryToken pins that a large query refused under load leaves the
// large-query token for the next one.
func TestLoadRefusalKeepsLargeQueryToken(t *testing.T) {
	limiter := rate.NewLimiter(0, 1)

	require.ErrorContains(t, checkLogQueryAdmission(RPSLimitThreshold+1, limiter, admissionMetrics(81, 0)), "server too busy")
	require.ErrorContains(t, checkLogQueryAdmission(RPSLimitThreshold+1, limiter, admissionMetrics(0, 16)), "server I/O saturated")

	require.NoError(t, checkLogQueryAdmission(RPSLimitThreshold+1, limiter, admissionMetrics(0, 0)))
	require.ErrorContains(t, checkLogQueryAdmission(RPSLimitThreshold+1, limiter, admissionMetrics(0, 0)), "rate limit exceeded")
	require.NoError(t, checkLogQueryAdmission(RPSLimitThreshold, limiter, admissionMetrics(0, 0)))
}

// TestNewLargeQueryLimiter pins the configured burst and that a zero rate disables the limit.
func TestNewLargeQueryLimiter(t *testing.T) {
	for _, tc := range []struct {
		rps, admitted int
	}{{rps: 0, admitted: 1000}, {rps: 3, admitted: 3}} {
		limiter := newLargeQueryLimiter(tc.rps)
		var admitted int
		for range 1000 {
			if checkLogQueryAdmission(RPSLimitThreshold+1, limiter, admissionMetrics(0, 0)) == nil {
				admitted++
			}
		}
		require.Equal(t, tc.admitted, admitted, "rps %d", tc.rps)
	}
}
