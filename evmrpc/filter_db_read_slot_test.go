package evmrpc

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/eth/filters"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/stretchr/testify/require"
)

const blockStorePanic = "error from proto block: wrong Header.LastCommitHash"

// panickingBlockTestClient panics when asked for panicHeight, the way the block store does when it
// cannot decode a stored block.
type panickingBlockTestClient struct {
	*heightTestClient
	panicHeight int64
}

func (c *panickingBlockTestClient) Block(ctx context.Context, height *int64) (*coretypes.ResultBlock, error) {
	if height != nil && *height == c.panicHeight {
		panic(blockStorePanic)
	}
	return c.heightTestClient.Block(ctx, height)
}

// newPanickingBlockFetcher returns a LogFetcher over blocks 1 to latest whose block store panics at
// panicHeight. It has a single DB-read slot, so a leaked slot stalls every later read.
func newPanickingBlockFetcher(latest, panicHeight int64) *LogFetcher {
	client := &panickingBlockTestClient{heightTestClient: newHeightTestClient(latest+10, 1, latest), panicHeight: panicHeight}
	return &LogFetcher{
		tmClient:           client,
		dbReadSemaphore:    make(chan struct{}, 1),
		globalBlockCache:   NewBlockCache(int(latest)),
		cacheCreationMutex: &sync.Mutex{},
		watermarks:         newHeightTestWatermarks(client, latest),
	}
}

// A block read that panics must free its DB-read slot, or GetLogs ends up rejecting every request as
// I/O saturated, and must come back as an error rather than escape the batch.
func TestProcessBatchReportsPanickingBlockReadAndFreesDBReadSlot(t *testing.T) {
	t.Parallel()

	const panicHeight = int64(10)
	fetcher := newPanickingBlockFetcher(90, panicHeight)
	res := make(chan *coretypes.ResultBlock, 1)
	errChan := make(chan error, 1)

	require.NotPanics(t, func() {
		fetcher.processBatch(context.Background(), panicHeight, panicHeight, filters.FilterCriteria{}, nil, res, errChan)
	})

	require.Empty(t, fetcher.dbReadSemaphore)
	require.Empty(t, res)
	require.Len(t, errChan, 1)
	err := <-errChan
	require.ErrorContains(t, err, "height 10")
	require.ErrorContains(t, err, blockStorePanic)
}

// A panic in one batch of a multi-batch request must fail the request. Left to the worker pool, which
// recovers it silently, the request would return the other batches' blocks as if they were the answer.
//
// Not parallel: it reads the global worker pool metrics, which parallel tests also move.
func TestFetchBlocksByCritFailsWhenABlockReadPanics(t *testing.T) {
	const (
		batchSize   = int64(evmrpcconfig.WorkerBatchSize)
		latest      = 3 * batchSize
		panicHeight = batchSize + batchSize/2 // inside the second of three batches
	)
	metrics := GetGlobalMetrics()
	slotsHeldBefore := metrics.DBSemaphoreAcquired.Load()
	panicsBefore := metrics.TasksPanicked.Load()
	fetcher := newPanickingBlockFetcher(latest, panicHeight)
	crit := filters.FilterCriteria{FromBlock: big.NewInt(1), ToBlock: big.NewInt(latest)}

	type fetchResult struct {
		blocks chan *coretypes.ResultBlock
		err    error
	}
	done := make(chan fetchResult, 1)
	go func() {
		blocks, _, err := fetcher.fetchBlocksByCrit(context.Background(), crit, 0, nil)
		done <- fetchResult{blocks: blocks, err: err}
	}()

	var got fetchResult
	select {
	case got = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("fetchBlocksByCrit did not return, most likely because a DB-read slot leaked")
	}

	require.ErrorContains(t, got.err, blockStorePanic)
	require.Nil(t, got.blocks)
	require.Empty(t, fetcher.dbReadSemaphore)
	require.Equal(t, slotsHeldBefore, metrics.DBSemaphoreAcquired.Load())
	require.Equal(t, panicsBefore+1, metrics.TasksPanicked.Load())
}
