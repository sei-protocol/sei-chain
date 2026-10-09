package evmrpc

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
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
	blocks, err := wm.EarliestHeight(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(500), blocks)
	receipts, _, err := wm.ReceiptRange(context.Background())
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

// "earliest" selects the synthetic genesis block until history is pruned past
// the chain's first block; 0x0 always selects it.
func TestIsGenesisBlockRequest(t *testing.T) {
	ctx := context.Background()
	unpruned := newHeightTestClient(0, 1, 10)
	pruned := newHeightTestClient(0, 500, 1000)

	for _, tc := range []struct {
		name   string
		client *heightTestClient
		number rpc.BlockNumber
		want   bool
	}{
		{"earliest unpruned", unpruned, rpc.EarliestBlockNumber, true},
		{"earliest pruned", pruned, rpc.EarliestBlockNumber, false},
		{"0x0 unpruned", unpruned, 0, true},
		{"0x0 pruned", pruned, 0, true},
		{"latest", unpruned, rpc.LatestBlockNumber, false},
		{"first block", unpruned, 1, false},
	} {
		got, err := isGenesisBlockRequest(ctx, tc.client, tc.number)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.want, got, tc.name)
	}

	earliest, err := earliestBlockHeight(ctx, unpruned)
	require.NoError(t, err)
	require.Equal(t, int64(1), earliest, "state and trace lookups resolve earliest to the first real block")
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

// statusFailsAfterClient answers Status successfully ok times, then errors.
type statusFailsAfterClient struct {
	*heightTestClient
	ok int
}

var errStatusUnavailable = errors.New("status unavailable")

func (c *statusFailsAfterClient) Status(ctx context.Context) (*coretypes.ResultStatus, error) {
	if c.ok == 0 {
		return nil, errStatusUnavailable
	}
	c.ok--
	return c.heightTestClient.Status(ctx)
}

// A failed receipt-range lookup is an error, not a floor of 0.
func TestGetLogsReturnsEarliestFloorLookupError(t *testing.T) {
	newFetcher := func() *LogFetcher {
		client := &statusFailsAfterClient{heightTestClient: newHeightTestClient(0, 5, 20), ok: 0}
		return &LogFetcher{
			tmClient:           client,
			k:                  newTestKeeperWithReceiptStore(),
			dbReadSemaphore:    make(chan struct{}, 1),
			globalBlockCache:   NewBlockCache(20),
			cacheCreationMutex: &sync.Mutex{},
			watermarks:         NewWatermarkManager(client, testCtxProvider, nil, &fakeReceiptStore{latest: 20, earliest: 8}),
		}
	}
	crit := filters.FilterCriteria{FromBlock: big.NewInt(rpc.EarliestBlockNumber.Int64()), ToBlock: big.NewInt(10)}

	_, _, err := newFetcher().GetLogsByFilters(context.Background(), crit, 0)
	require.ErrorIs(t, err, errStatusUnavailable)

	_, _, err = newFetcher().fetchBlocksByCrit(context.Background(), crit, 0, nil)
	require.ErrorIs(t, err, errStatusUnavailable)
}
