package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestIsHistoricalDebugTraceBlock(t *testing.T) {
	tests := []struct {
		name             string
		blockHeight      int64
		latestHeight     int64
		maxBlockLookback int64
		want             bool
	}{
		{
			name:             "older than configured lookback",
			blockHeight:      8,
			latestHeight:     10,
			maxBlockLookback: 1,
			want:             true,
		},
		{
			name:             "equal to configured lookback",
			blockHeight:      9,
			latestHeight:     10,
			maxBlockLookback: 1,
			want:             false,
		},
		{
			name:             "zero lookback treats previous block as historical",
			blockHeight:      9,
			latestHeight:     10,
			maxBlockLookback: 0,
			want:             true,
		},
		{
			name:             "zero lookback allows latest block",
			blockHeight:      10,
			latestHeight:     10,
			maxBlockLookback: 0,
			want:             false,
		},
		{
			name:             "negative lookback disables classification",
			blockHeight:      1,
			latestHeight:     10,
			maxBlockLookback: -1,
			want:             false,
		},
		{
			name:             "future block",
			blockHeight:      11,
			latestHeight:     10,
			maxBlockLookback: 0,
			want:             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isHistoricalDebugTraceBlock(tt.blockHeight, tt.latestHeight, tt.maxBlockLookback))
		})
	}
}

func TestGuardHistoricalDebugTraceHeight(t *testing.T) {
	latestCtx := sdk.Context{}.WithBlockHeight(10)
	api := &DebugAPI{
		ctxProvider:      func(int64) sdk.Context { return latestCtx },
		connectionType:   ConnectionTypeHTTP,
		maxBlockLookback: -1,
	}

	err := api.guardHistoricalDebugTraceHeight(context.Background(), "debug_traceBlockByNumber", 8)
	require.NoError(t, err)

	api.maxBlockLookback = 1
	err = api.guardHistoricalDebugTraceHeight(context.Background(), "debug_traceBlockByNumber", 8)
	require.Error(t, err)
	require.Contains(t, err.Error(), "block number 8 is beyond max lookback of 1")

	err = api.guardHistoricalDebugTraceHeight(context.Background(), "debug_traceBlockByNumber", 9)
	require.NoError(t, err)

	api.maxBlockLookback = 0
	err = api.guardHistoricalDebugTraceHeight(context.Background(), "debug_traceBlockByNumber", 9)
	require.Error(t, err)
	require.Contains(t, err.Error(), "block number 9 is beyond max lookback of 0")
}

func TestGuardTraceRequestByHashUsesTendermintHeight(t *testing.T) {
	latestHeight := int64(10)
	latestCtx := sdk.Context{}.WithBlockHeight(latestHeight)
	tmClient := newHeightTestClient(8, 1, latestHeight)
	rs := &fakeReceiptStore{latest: latestHeight, earliest: 1}
	api := &DebugAPI{
		tmClient:         tmClient,
		ctxProvider:      func(int64) sdk.Context { return latestCtx },
		connectionType:   ConnectionTypeHTTP,
		maxBlockLookback: 1,
		backend: &Backend{
			watermarks: NewWatermarkManager(tmClient, func(int64) sdk.Context { return latestCtx }, nil, rs),
		},
	}

	err := api.guardTraceRequestByHash(t.Context(), "debug_traceBlockByHash", common.HexToHash(highBlockHashHex))
	require.Error(t, err)
	require.Contains(t, err.Error(), "block number 8 is beyond max lookback of 1")

	err = api.guardTraceCallRequestByHash(t.Context(), "debug_traceCall", common.HexToHash("0x1"))
	require.NoError(t, err)
}

func TestEnsureTraceCallHeightAvailableIgnoresReceipts(t *testing.T) {
	t.Parallel()

	parentHeight := int64(149)
	receiptFloor := int64(150)
	latestHeight := int64(200)
	latestCtx := sdk.Context{}.WithBlockHeight(latestHeight)
	tmClient := newHeightTestClient(parentHeight, 1, latestHeight)
	rs := &fakeReceiptStore{latest: latestHeight, earliest: receiptFloor}
	stateStore := &fakeStateStore{latest: latestHeight, earliest: 1}
	wm := NewWatermarkManager(tmClient, func(int64) sdk.Context { return latestCtx }, stateStore, rs)

	require.ErrorContains(t, wm.EnsureTraceHeightAvailable(t.Context(), parentHeight), "receipts have been pruned")
	require.NoError(t, wm.EnsureTraceCallHeightAvailable(t.Context(), parentHeight))
}

func TestEnsureTraceCallHeightAvailableUsesStateAtHeight(t *testing.T) {
	t.Parallel()

	stateFloor := int64(150)
	latestHeight := int64(200)
	latestCtx := sdk.Context{}.WithBlockHeight(latestHeight)
	tmClient := newHeightTestClient(stateFloor, 1, latestHeight)
	rs := &fakeReceiptStore{latest: latestHeight, earliest: 1}
	stateStore := &fakeStateStore{latest: latestHeight, earliest: stateFloor}
	wm := NewWatermarkManager(tmClient, func(int64) sdk.Context { return latestCtx }, stateStore, rs)

	require.ErrorContains(t, wm.EnsureTraceHeightAvailable(t.Context(), stateFloor), "has been pruned")
	require.NoError(t, wm.EnsureTraceCallHeightAvailable(t.Context(), stateFloor))
}

func TestEnsureTraceHeightAvailableReceiptsPruned(t *testing.T) {
	t.Parallel()

	client := newHeightTestClient(100, 1, 200)
	rs := &fakeReceiptStore{latest: 200, earliest: 150}
	wm := NewWatermarkManager(client, watermarkTestCtxProvider(200), nil, rs)

	err := wm.EnsureTraceHeightAvailable(t.Context(), 100)
	require.Error(t, err)
	require.Contains(t, err.Error(), "receipts have been pruned")
}

func TestEnsureTraceHeightAvailableStatePruned(t *testing.T) {
	t.Parallel()

	client := newHeightTestClient(100, 1, 200)
	stateStore := &fakeStateStore{latest: 200, earliest: 150}
	rs := &fakeReceiptStore{latest: 200, earliest: 1}
	wm := NewWatermarkManager(client, watermarkTestCtxProvider(200), stateStore, rs)

	err := wm.EnsureTraceHeightAvailable(t.Context(), 100)
	require.Error(t, err)
	require.Contains(t, err.Error(), "has been pruned")
}

func TestTraceReceiptFloorBoundary(t *testing.T) {
	t.Parallel()

	// Receipt retention starts at 150; block 149 still exists in Tendermint but
	// its receipts are gone. Trace guard applies at the requested height only —
	// parent fetches at height-1 for state replay must not re-run it.
	parentHeight := int64(149)
	receiptFloor := int64(150)
	latestHeight := int64(200)
	latestCtx := sdk.Context{}.WithBlockHeight(latestHeight)
	tmClient := newHeightTestClient(parentHeight, 1, latestHeight)
	rs := &fakeReceiptStore{latest: latestHeight, earliest: receiptFloor}
	stateStore := &fakeStateStore{latest: latestHeight, earliest: 1}
	wm := NewWatermarkManager(tmClient, func(int64) sdk.Context { return latestCtx }, stateStore, rs)

	require.ErrorContains(t, wm.EnsureTraceHeightAvailable(t.Context(), parentHeight), "receipts have been pruned")
	require.NoError(t, wm.EnsureTraceHeightAvailable(t.Context(), receiptFloor))
}

func TestTraceBlockByNumberReceiptPrunedBeforeSemaphore(t *testing.T) {
	t.Parallel()

	prunedHeight := int64(100)
	latestHeight := int64(200)
	latestCtx := sdk.Context{}.WithBlockHeight(latestHeight)
	tmClient := newHeightTestClient(prunedHeight, 1, latestHeight)
	rs := &fakeReceiptStore{latest: latestHeight, earliest: 150}
	api := &DebugAPI{
		tmClient:           tmClient,
		ctxProvider:        func(int64) sdk.Context { return latestCtx },
		connectionType:     ConnectionTypeHTTP,
		maxBlockLookback:   -1,
		traceCallSemaphore: make(chan struct{}, 1),
		traceTimeout:       time.Second,
		backend: &Backend{
			tmClient:   tmClient,
			watermarks: NewWatermarkManager(tmClient, func(int64) sdk.Context { return latestCtx }, nil, rs),
		},
	}

	api.traceCallSemaphore <- struct{}{}
	defer func() { <-api.traceCallSemaphore }()

	_, err := api.TraceBlockByNumber(t.Context(), rpc.BlockNumber(prunedHeight), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "receipts have been pruned")
	require.NotErrorIs(t, err, errTraceConcurrencyLimit)
}

func TestBlockByNumberLatestUsesSafeLatestWatermark(t *testing.T) {
	t.Parallel()

	const (
		safeLatest = int64(99)
		ctxTip     = int64(100)
	)
	latestCtx := sdk.Context{}.WithBlockHeight(ctxTip)
	tmClient := newHeightTestClient(safeLatest, 1, ctxTip)
	rs := &fakeReceiptStore{latest: safeLatest, earliest: 1}
	stateStore := &fakeStateStore{latest: safeLatest, earliest: 1}
	wm := NewWatermarkManager(tmClient, func(int64) sdk.Context { return latestCtx }, stateStore, rs)

	blockNumberPtr, err := getBlockNumber(t.Context(), tmClient, rpc.LatestBlockNumber)
	require.NoError(t, err)
	require.Nil(t, blockNumberPtr)

	tmBlock, err := blockByNumberRespectingWatermarks(t.Context(), tmClient, wm, blockNumberPtr, 1)
	require.NoError(t, err)
	require.Equal(t, safeLatest, tmBlock.Block.Height)

	rawTip := ctxTip
	_, err = blockByNumberRespectingWatermarks(t.Context(), tmClient, wm, &rawTip, 1)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBlockHeightNotYetAvailable)
}

func TestTraceLatestTagGuardMatchesBlockResolution(t *testing.T) {
	t.Parallel()

	const (
		safeLatest = int64(99)
		ctxTip     = int64(100)
	)
	latestCtx := sdk.Context{}.WithBlockHeight(ctxTip)
	tmClient := newHeightTestClient(safeLatest, 1, ctxTip)
	rs := &fakeReceiptStore{latest: safeLatest, earliest: 1}
	stateStore := &fakeStateStore{latest: safeLatest, earliest: 1}
	wm := NewWatermarkManager(tmClient, func(int64) sdk.Context { return latestCtx }, stateStore, rs)
	api := &DebugAPI{
		tmClient:    tmClient,
		ctxProvider: func(int64) sdk.Context { return latestCtx },
		backend: &Backend{
			tmClient:   tmClient,
			watermarks: wm,
		},
	}

	guardHeight, err := api.resolveDebugTraceBlockNumber(t.Context(), rpc.LatestBlockNumber)
	require.NoError(t, err)
	require.Equal(t, safeLatest, guardHeight)

	blockNumberPtr, err := getBlockNumber(t.Context(), tmClient, rpc.LatestBlockNumber)
	require.NoError(t, err)
	tmBlock, err := blockByNumberRespectingWatermarks(t.Context(), tmClient, wm, blockNumberPtr, 1)
	require.NoError(t, err)
	require.Equal(t, guardHeight, tmBlock.Block.Height)
}

type traceGuardReceiptStore struct {
	fakeReceiptStore
	getReceiptErr error
}

func (s *traceGuardReceiptStore) GetReceipt(_ sdk.Context, _ common.Hash) (*evmtypes.Receipt, error) {
	if s.getReceiptErr != nil {
		return nil, s.getReceiptErr
	}
	return nil, receipt.ErrNotFound
}

func TestGuardTraceRequestByTxHashReceiptLookupErrors(t *testing.T) {
	t.Parallel()

	const latestHeight = int64(10)
	latestCtx := sdk.Context{}.WithBlockHeight(latestHeight)
	txHash := common.HexToHash("0xabc")
	storeErr := errors.New("receipt store unavailable")

	newAPI := func(store receipt.ReceiptStore) *DebugAPI {
		k := &keeper.Keeper{}
		k.SetReceiptStoreForTesting(store)
		return &DebugAPI{
			keeper:           k,
			ctxProvider:      func(int64) sdk.Context { return latestCtx },
			maxBlockLookback: -1,
		}
	}

	t.Run("ErrReceiptPruned", func(t *testing.T) {
		t.Parallel()
		prunedErr := fmt.Errorf("requested height 100 receipts have been pruned; earliest available is 150: %w", receipt.ErrReceiptPruned)
		api := newAPI(&traceGuardReceiptStore{getReceiptErr: prunedErr})
		err := api.guardTraceRequestByTxHash(t.Context(), "debug_traceTransaction", txHash)
		require.ErrorIs(t, err, receipt.ErrReceiptPruned)
	})

	t.Run("store error", func(t *testing.T) {
		t.Parallel()
		api := newAPI(&traceGuardReceiptStore{getReceiptErr: storeErr})
		err := api.guardTraceRequestByTxHash(t.Context(), "debug_traceTransaction", txHash)
		require.ErrorIs(t, err, storeErr)
	})

	t.Run("ErrNotFound falls through to lookback", func(t *testing.T) {
		t.Parallel()
		api := newAPI(&traceGuardReceiptStore{getReceiptErr: receipt.ErrNotFound})
		err := api.guardTraceRequestByTxHash(t.Context(), "debug_traceTransaction", txHash)
		require.NoError(t, err)
	})

	t.Run("ErrNotConfigured", func(t *testing.T) {
		t.Parallel()
		api := newAPI(&traceGuardReceiptStore{getReceiptErr: receipt.ErrNotConfigured})
		err := api.guardTraceRequestByTxHash(t.Context(), "debug_traceTransaction", txHash)
		require.ErrorIs(t, err, receipt.ErrNotConfigured)
	})
}
