package rpc

import (
	"net/http/httptest"
	"runtime"
	"testing"

	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-cosmos/version"
)

func TestHandlerServesClientVersion(t *testing.T) {
	handler, err := newHandler(&testBackend{}, evmonly.NewMemoryReceiptStore(), noRPCLimits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	platform := runtime.GOOS + "-" + runtime.GOARCH + "/" + runtime.Version()

	for _, tc := range []struct {
		name    string
		version string
		want    string
	}{
		{name: "built with a version", version: "v6.7.0", want: "Sei/v6.7.0/" + platform},
		{name: "built without a version", version: "", want: "Sei/unknown/" + platform},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := version.Version
			t.Cleanup(func() { version.Version = original })
			version.Version = tc.version

			var got string
			require.NoError(t, client.CallContext(t.Context(), &got, "web3_clientVersion"))
			require.Equal(t, tc.want, got)
		})
	}
}
