package rpc

import (
	"net/http/httptest"
	"testing"

	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

func TestHandlerServesClientVersionLikeRegularRPC(t *testing.T) {
	handler, err := newHandler(&testBackend{}, evmonly.NewMemoryReceiptStore(), noRPCLimits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got string
	require.NoError(t, client.CallContext(t.Context(), &got, "web3_clientVersion"))
	require.Equal(t, (&evmrpc.Web3API{}).ClientVersion(), got)
}
