package rpc

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestWebsocketEndToEnd(t *testing.T) {
	backend := &testBackend{blockNumber: func() uint64 { return 42 }, chainID: func() uint64 { return 713700 }}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), noRPCLimits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(websocketHandler(handler))
	t.Cleanup(server.Close)
	client, err := ethrpc.DialWebsocket(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"), "http://example.com")
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got hexutil.Uint64
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_blockNumber"))
	require.Equal(t, hexutil.Uint64(42), got)

	batch := []ethrpc.BatchElem{
		{Method: "eth_blockNumber", Result: new(hexutil.Uint64)},
		{Method: "eth_chainId", Result: new(hexutil.Uint64)},
	}
	require.NoError(t, client.BatchCallContext(t.Context(), batch))
	for _, elem := range batch {
		require.NoError(t, elem.Error)
	}
	require.Equal(t, hexutil.Uint64(42), *batch[0].Result.(*hexutil.Uint64))
	require.Equal(t, hexutil.Uint64(713700), *batch[1].Result.(*hexutil.Uint64))
}

func TestWebsocketRejectsPlainHTTP(t *testing.T) {
	backend := &testBackend{blockNumber: func() uint64 { return 42 }}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), noRPCLimits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(websocketHandler(handler))
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got hexutil.Uint64
	require.Error(t, client.CallContext(t.Context(), &got, "eth_blockNumber"))
}

func TestHTTPBatchResolvesCurrentStateTagsToOneHead(t *testing.T) {
	var head atomic.Int64
	head.Store(10)
	backend := fixedGasLimitBackend(t, 100_000, func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		height := head.Load()
		if req.Height != nil {
			height = int64(*req.Height)
		}
		// A block commits after every read, so each batch call sees a newer head.
		head.Add(1)
		return &coretypes.ResultBlock{Block: &tmtypes.Block{Header: tmtypes.Header{Height: height, Time: time.Unix(1_700_000_000, 0)}}}, nil
	})
	backend.blockNumber = func() uint64 { return uint64(head.Load()) } //nolint:gosec // G115: the test head is positive.
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetLatestVersion(100))
	handler, err := newHandler(backend, store, noRPCLimits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(pinHeadPerRequest(backend, handler))
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	type header struct {
		Number hexutil.Uint64 `json:"number"`
	}
	var batch []ethrpc.BatchElem
	for _, tag := range []string{"latest", "safe", "finalized", "pending"} {
		batch = append(batch, ethrpc.BatchElem{Method: "eth_getBlockByNumber", Args: []any{tag, false}, Result: new(header)})
	}
	batch = append(batch, ethrpc.BatchElem{Method: "eth_blockNumber", Result: new(hexutil.Uint64)})
	require.NoError(t, client.BatchCallContext(t.Context(), batch))
	for _, elem := range batch {
		require.NoError(t, elem.Error)
	}
	latest := batch[0].Result.(*header).Number
	for _, elem := range batch[1:4] {
		require.Equal(t, latest, elem.Result.(*header).Number, elem.Args[0])
	}
	require.Equal(t, latest, *batch[4].Result.(*hexutil.Uint64))
}
