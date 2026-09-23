package rpc

import (
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

func TestBlockNumber(t *testing.T) {
	backend := &testBackend{blockNumber: func() uint64 { return 42 }}
	api := &infoAPI{backend: backend}
	require.Equal(t, hexutil.Uint64(42), api.BlockNumber(t.Context()))
}

func TestBlockNumberEndToEnd(t *testing.T) {
	backend := &testBackend{blockNumber: func() uint64 { return 42 }}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got hexutil.Uint64
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_blockNumber"))
	require.Equal(t, hexutil.Uint64(42), got)
}

func TestChainId(t *testing.T) {
	backend := &testBackend{chainID: func() uint64 { return 713715 }}
	api := &infoAPI{backend: backend}
	require.Equal(t, (*hexutil.Big)(big.NewInt(713715)), api.ChainId(t.Context()))
}

func TestChainIdEndToEnd(t *testing.T) {
	backend := &testBackend{chainID: func() uint64 { return 713715 }}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got hexutil.Big
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_chainId"))
	require.Equal(t, *big.NewInt(713715), big.Int(got))
}
