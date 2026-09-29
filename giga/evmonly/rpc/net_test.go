package rpc

import (
	"net/http/httptest"
	"testing"

	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

func TestNetVersion(t *testing.T) {
	api := &netAPI{backend: &testBackend{
		chainID: func() uint64 { return 1329 },
	}}

	require.Equal(t, "1329", api.Version(t.Context()))
}

func TestHandlerServesNetVersion(t *testing.T) {
	backend := &testBackend{
		chainID: func() uint64 { return 1329 },
	}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore())
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got string
	require.NoError(t, client.CallContext(t.Context(), &got, "net_version"))
	require.Equal(t, "1329", got)
}
