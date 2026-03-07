package evmrpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPrepareTraceContextFailsFastWhenSemaphoreIsFull(t *testing.T) {
	t.Parallel()

	api := &DebugAPI{
		traceCallSemaphore: make(chan struct{}, 1),
		traceTimeout:       time.Second,
	}

	release, err := api.acquireTraceSemaphore(context.Background())
	require.NoError(t, err)
	defer release()

	start := time.Now()
	traceCtx, done, err := api.prepareTraceContext(context.Background())
	elapsed := time.Since(start)

	require.Nil(t, traceCtx)
	require.Nil(t, done)
	require.ErrorIs(t, err, errTraceConcurrencyLimit)
	require.Less(t, elapsed, 100*time.Millisecond)
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

func TestAcquireTraceSemaphoreCanceledContextDoesNotConsumeSlot(t *testing.T) {
	t.Parallel()

	api := &DebugAPI{
		traceCallSemaphore: make(chan struct{}, 1),
	}

	for i := 0; i < 256; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		release, err := api.acquireTraceSemaphore(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.NotNil(t, release)

		select {
		case api.traceCallSemaphore <- struct{}{}:
			<-api.traceCallSemaphore
		default:
			t.Fatal("expected canceled acquire to leave semaphore slot available")
		}
	}
}
