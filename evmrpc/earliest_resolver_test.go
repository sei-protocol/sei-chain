package evmrpc

import (
	"context"
	"math/big"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

// A node whose history starts above genesis (pruning, a Giga cutover or SIP-3)
// reports that height as "earliest", and for 0x0.
func TestEarliestResolvesToLowestAvailableBlock(t *testing.T) {
	tmClient := newHeightTestClient(0, 500, 1000)

	earliest, err := getBlockNumber(context.Background(), tmClient, rpc.EarliestBlockNumber)
	require.NoError(t, err)
	require.Equal(t, int64(500), *earliest)

	zero, err := getBlockNumber(context.Background(), tmClient, 0)
	require.NoError(t, err)
	require.Equal(t, int64(500), *zero)

	ctxProvider := func(int64) sdk.Context { return sdk.Context{}.WithBlockHeight(1000) }
	wm := NewWatermarkManager(tmClient, ctxProvider, nil, nil)
	blocks, err := wm.EarliestAvailable(context.Background(), BlockHistory)
	require.NoError(t, err)
	require.Equal(t, int64(500), blocks)
	receipts, err := wm.EarliestAvailable(context.Background(), ReceiptHistory)
	require.NoError(t, err)
	require.Equal(t, int64(500), receipts)
}

// The earliest block is never below the chain's initial height, even when
// Tendermint has not reported one yet.
func TestEarliestBlockHeightFlooredAtInitialHeight(t *testing.T) {
	earliest, err := earliestBlockHeight(context.Background(), newHeightTestClient(0, 0, 10))
	require.NoError(t, err)
	require.Equal(t, int64(1), earliest)
}

// "earliest" and 0x0 log bounds mean "from the start of available history"
// and clamp to the floor; an explicit height below the floor is still rejected
// rather than silently truncated.
func TestComputeBlockBoundsClampsEarliest(t *testing.T) {
	const latest, floor = int64(1000), int64(500)
	for _, from := range []*big.Int{big.NewInt(0), big.NewInt(rpc.EarliestBlockNumber.Int64())} {
		begin, end, err := ComputeBlockBounds(latest, floor, 0, filters.FilterCriteria{FromBlock: from, ToBlock: big.NewInt(rpc.LatestBlockNumber.Int64())})
		require.NoError(t, err)
		require.Equal(t, floor, begin)
		require.Equal(t, latest, end)
	}
	begin, end, err := ComputeBlockBounds(latest, floor, 0, filters.FilterCriteria{FromBlock: big.NewInt(0), ToBlock: big.NewInt(rpc.EarliestBlockNumber.Int64())})
	require.NoError(t, err)
	require.Equal(t, floor, begin)
	require.Equal(t, floor, end)

	_, _, err = ComputeBlockBounds(latest, floor, 0, filters.FilterCriteria{FromBlock: big.NewInt(100)})
	require.ErrorContains(t, err, "before earliest available block 500")
}

// The block-by-block log fallback starts "earliest" at the receipt floor, not the block floor.
func TestFetchBlocksByCritClampsEarliestToReceiptFloor(t *testing.T) {
	client := newHeightTestClient(0, 5, 20)
	fetcher := &LogFetcher{
		tmClient:           client,
		dbReadSemaphore:    make(chan struct{}, 1),
		globalBlockCache:   NewBlockCache(20),
		cacheCreationMutex: &sync.Mutex{},
		watermarks:         NewWatermarkManager(client, testCtxProvider, nil, &fakeReceiptStore{latest: 20, earliest: 8}),
	}
	crit := filters.FilterCriteria{FromBlock: big.NewInt(rpc.EarliestBlockNumber.Int64()), ToBlock: big.NewInt(10)}

	blocks, _, err := fetcher.fetchBlocksByCrit(context.Background(), crit, 0, nil)
	require.NoError(t, err)
	var heights []int64
	for b := range blocks {
		heights = append(heights, b.Block.Height)
	}
	require.ElementsMatch(t, []int64{8, 9, 10}, heights)
}
