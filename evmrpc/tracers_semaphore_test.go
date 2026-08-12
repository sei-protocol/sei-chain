package evmrpc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/rpc"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
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

func TestTraceBlockByNumberReleasesSemaphoreOnPanic(t *testing.T) {
	t.Parallel()

	latestCtx := sdk.Context{}.WithBlockHeight(10)
	api := &DebugAPI{
		ctxProvider:        func(int64) sdk.Context { return latestCtx },
		traceCallSemaphore: make(chan struct{}, 1),
		traceTimeout:       time.Second,
		maxBlockLookback:   -1,
	}

	// The nil keeper panics in the cache lookup after the trace context has
	// acquired the only semaphore slot.
	for range 3 {
		require.Panics(t, func() {
			_, _ = api.TraceBlockByNumber(context.Background(), rpc.BlockNumber(10), nil)
		})
	}

	release, err := api.acquireTraceSemaphore(context.Background())
	require.NoError(t, err)
	release()
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

func TestPrepareTraceContextReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	api := &DebugAPI{
		traceCallSemaphore: make(chan struct{}, 1),
		traceTimeout:       time.Second,
	}

	_, done, err := api.prepareTraceContext(context.Background())
	require.NoError(t, err)

	doubleDone := make(chan struct{})
	go func() {
		done()
		done()
		close(doubleDone)
	}()
	select {
	case <-doubleDone:
	case <-time.After(2 * time.Second):
		t.Fatal("expected repeated cleanup calls to return immediately")
	}

	require.Empty(t, api.traceCallSemaphore, "expected cleanup to release the slot exactly once")
}

// TestTraceSemaphoreSlotFreedWhenTraceOutlivesContext reproduces the permanent slot
// leak from issue #3900: a trace that never runs its deferred cleanup (wedged on an
// uninterruptible lock, or stalled while unwinding a panic) must not consume its
// semaphore slot forever. The slot has to return to the pool once the trace context
// ends.
func TestTraceSemaphoreSlotFreedWhenTraceOutlivesContext(t *testing.T) {
	t.Parallel()

	api := &DebugAPI{
		traceCallSemaphore: make(chan struct{}, 1),
		traceTimeout:       20 * time.Millisecond,
	}

	// Acquire a slot and deliberately never call done, simulating a trace
	// goroutine that wedges before its deferred cleanup can run.
	traceCtx, _, err := api.prepareTraceContext(context.Background())
	require.NoError(t, err)

	<-traceCtx.Done()

	require.Eventually(t, func() bool {
		select {
		case api.traceCallSemaphore <- struct{}{}:
			<-api.traceCallSemaphore
			return true
		default:
			return false
		}
	}, 2*time.Second, 5*time.Millisecond, "expected slot to return to the pool once the trace context ended")
}

// blockingHashLookupClient wedges BlockByHash until unblock is closed, standing in
// for a trace stuck on an uninterruptible dependency (e.g. the global wasm VM mutex).
type blockingHashLookupClient struct {
	*heightTestClient
	lookupStarted chan struct{}
	unblock       chan struct{}
	startedOnce   sync.Once
}

func (c *blockingHashLookupClient) BlockByHash(ctx context.Context, hash tmbytes.HexBytes) (*coretypes.ResultBlock, error) {
	c.startedOnce.Do(func() { close(c.lookupStarted) })
	<-c.unblock
	return c.heightTestClient.BlockByHash(ctx, hash)
}

func TestTraceBlockByHashWedgedCallDoesNotExhaustSemaphore(t *testing.T) {
	latestHeight := int64(10)
	latestCtx := sdk.Context{}.WithBlockHeight(latestHeight)
	tmClient := &blockingHashLookupClient{
		heightTestClient: newHeightTestClient(8, 1, latestHeight),
		lookupStarted:    make(chan struct{}),
		unblock:          make(chan struct{}),
	}
	watermarks := NewWatermarkManager(tmClient, func(int64) sdk.Context { return latestCtx }, nil, &fakeReceiptStore{latest: latestHeight})
	api := &DebugAPI{
		tmClient:           tmClient,
		ctxProvider:        func(int64) sdk.Context { return latestCtx },
		connectionType:     ConnectionTypeHTTP,
		traceCallSemaphore: make(chan struct{}, 1),
		traceTimeout:       20 * time.Millisecond,
		maxBlockLookback:   1,
		backend: &Backend{
			tmClient:   tmClient,
			watermarks: watermarks,
		},
	}

	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		// The wedged handler must not take the test binary down if it panics
		// after being unblocked; the semaphore assertions below are the test.
		defer func() { _ = recover() }()
		_, _ = api.TraceBlockByHash(context.Background(), common.HexToHash(highBlockHashHex), nil)
	}()

	<-tmClient.lookupStarted

	// The handler goroutine is wedged holding its slot. Once the trace context
	// deadline passes, the slot must return to the pool so subsequent traces
	// are not rejected with "server busy" forever.
	require.Eventually(t, func() bool {
		release, err := api.acquireTraceSemaphore(context.Background())
		if err != nil {
			return false
		}
		release()
		return true
	}, 2*time.Second, 5*time.Millisecond, "expected slot to be freed after the wedged trace timed out")

	close(tmClient.unblock)
	<-handlerDone

	// The wedged handler's deferred cleanup ran after the backstop already
	// released the slot; occupancy must still be exactly zero.
	require.Empty(t, api.traceCallSemaphore)
}

// TestTraceBlockByHashPanicReleasesSemaphore pins the "released on every code path,
// including panic recovery" contract from issue #3900.
func TestTraceBlockByHashPanicReleasesSemaphore(t *testing.T) {
	latestHeight := int64(10)
	latestCtx := sdk.Context{}.WithBlockHeight(latestHeight)
	tmClient := &panicHashLookupClient{
		heightTestClient: newHeightTestClient(8, 1, latestHeight),
	}
	watermarks := NewWatermarkManager(tmClient, func(int64) sdk.Context { return latestCtx }, nil, &fakeReceiptStore{latest: latestHeight})
	api := &DebugAPI{
		tmClient:           tmClient,
		ctxProvider:        func(int64) sdk.Context { return latestCtx },
		connectionType:     ConnectionTypeHTTP,
		traceCallSemaphore: make(chan struct{}, 1),
		traceTimeout:       time.Second,
		backend: &Backend{
			tmClient:   tmClient,
			watermarks: watermarks,
		},
	}

	// The panic escapes the handler (recordMetricsWithError re-panics after
	// recording) exactly as it would toward the RPC server's recovery layer.
	require.Panics(t, func() {
		_, _ = api.TraceBlockByHash(context.Background(), common.HexToHash(highBlockHashHex), nil)
	})

	require.Empty(t, api.traceCallSemaphore, "expected the panicking trace to release its slot")
}

type panicHashLookupClient struct {
	*heightTestClient
}

func (c *panicHashLookupClient) BlockByHash(context.Context, tmbytes.HexBytes) (*coretypes.ResultBlock, error) {
	panic("hash lookup should not happen before trace context setup")
}

func TestHashBasedTraceEndpointsAcquireSemaphoreBeforeHashLookup(t *testing.T) {
	latestHeight := int64(10)
	latestCtx := sdk.Context{}.WithBlockHeight(latestHeight)
	tmClient := &panicHashLookupClient{
		heightTestClient: newHeightTestClient(8, 1, latestHeight),
	}
	watermarks := NewWatermarkManager(tmClient, func(int64) sdk.Context { return latestCtx }, nil, &fakeReceiptStore{latest: latestHeight})
	api := &DebugAPI{
		tmClient:           tmClient,
		ctxProvider:        func(int64) sdk.Context { return latestCtx },
		connectionType:     ConnectionTypeHTTP,
		traceCallSemaphore: make(chan struct{}, 1),
		traceTimeout:       time.Second,
		backend: &Backend{
			tmClient:   tmClient,
			watermarks: watermarks,
		},
	}

	api.traceCallSemaphore <- struct{}{}
	defer func() { <-api.traceCallSemaphore }()

	hash := common.HexToHash(highBlockHashHex)
	_, err := api.TraceBlockByHash(context.Background(), hash, nil)
	require.ErrorIs(t, err, errTraceConcurrencyLimit)

	blockNrOrHash := rpc.BlockNumberOrHashWithHash(hash, false)
	_, err = api.TraceCall(context.Background(), export.TransactionArgs{}, blockNrOrHash, nil)
	require.ErrorIs(t, err, errTraceConcurrencyLimit)
}
