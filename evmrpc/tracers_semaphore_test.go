package evmrpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPrepareTraceContextTimesOutWhileWaitingForSemaphore(t *testing.T) {
	t.Parallel()

	api := &DebugAPI{
		traceCallSemaphore: make(chan struct{}, 1),
		traceTimeout:       20 * time.Millisecond,
	}

	release, err := api.acquireTraceSemaphore(context.Background())
	require.NoError(t, err)
	defer release()

	start := time.Now()
	traceCtx, done, err := api.prepareTraceContext(context.Background())
	elapsed := time.Since(start)

	require.Nil(t, traceCtx)
	require.Nil(t, done)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.GreaterOrEqual(t, elapsed, api.traceTimeout)
	require.Less(t, elapsed, 500*time.Millisecond)
}

func TestPrepareTraceContextReleasesSemaphoreOnCleanup(t *testing.T) {
	t.Parallel()

	api := &DebugAPI{
		traceCallSemaphore: make(chan struct{}, 1),
		traceTimeout:       time.Second,
	}

	traceCtx, done, err := api.prepareTraceContext(context.Background())
	require.NoError(t, err)
	require.NotNil(t, traceCtx)
	require.NotNil(t, done)

	select {
	case api.traceCallSemaphore <- struct{}{}:
		t.Fatal("expected semaphore to be held by active trace context")
	default:
	}

	done()

	select {
	case api.traceCallSemaphore <- struct{}{}:
		<-api.traceCallSemaphore
	default:
		t.Fatal("expected cleanup to release the semaphore")
	}
}
