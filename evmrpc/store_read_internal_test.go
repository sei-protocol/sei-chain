package evmrpc

import (
	"context"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestReadStoresPrefersRequestError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	result, err := readStores(ctx, func(int64) sdk.Context { return sdk.Context{} }, func(func(int64) sdk.Context) (string, error) {
		cancel()
		return "late result", nil
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, result)
}

func TestReadStoreAtHeightSkipsReadAfterContextConstructionCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	readStarted := false
	_, err := readStoreAtHeight(ctx, 1, func(int64) sdk.Context {
		cancel()
		return sdk.Context{}
	}, func(sdk.Context) (string, error) {
		readStarted = true
		return "late result", nil
	})

	require.ErrorIs(t, err, context.Canceled)
	require.False(t, readStarted)
}

func TestCachedReceiptRejectsExpiredRequest(t *testing.T) {
	const height int64 = 7
	txHash := common.HexToHash("0x1")
	receipt := &evmtypes.Receipt{BlockNumber: uint64(height)} //nolint:gosec
	block := &coretypes.ResultBlock{Block: &tmtypes.Block{Header: tmtypes.Header{Height: height}}}
	cache := NewBlockCache(1)
	cache.Add(height, &BlockCacheEntry{
		Block:    block,
		Receipts: map[common.Hash]*evmtypes.Receipt{txHash: receipt},
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	sdkCtx := sdk.Context{}.WithContext(ctx)
	result, err := getOrSetCachedReceiptErr(&sync.Mutex{}, cache, sdkCtx, nil, block, txHash)

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, result)
}
