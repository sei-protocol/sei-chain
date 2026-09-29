package rpc

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

func TestWebsocketEndToEnd(t *testing.T) {
	backend := &testBackend{blockNumber: func() uint64 { return 42 }, chainID: func() uint64 { return 713715 }}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore())
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
	require.Equal(t, hexutil.Uint64(713715), *batch[1].Result.(*hexutil.Uint64))
}

func TestWebsocketRejectsPlainHTTP(t *testing.T) {
	backend := &testBackend{blockNumber: func() uint64 { return 42 }}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore())
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
