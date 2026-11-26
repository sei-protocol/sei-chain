package rpcerr_test

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/rpcerr"
)

var errCause = errors.New("cause")

// failingService returns the error its method is named for.
type failingService struct{}

func (failingService) ServerError() error   { return rpcerr.ServerError(errCause) }
func (failingService) InvalidParams() error { return rpcerr.InvalidParams(errCause) }
func (failingService) LimitExceeded() error { return rpcerr.LimitExceeded(errCause) }
func (failingService) Reverted() error      { return rpcerr.ExecutionReverted([]byte{0xde, 0xad}) }
func (failingService) Wrapped() error {
	return fmt.Errorf("wrapped: %w", rpcerr.InvalidParams(errCause))
}

// dial serves failingService over HTTP and returns a client of it.
func dial(t *testing.T) *ethrpc.Client {
	t.Helper()
	server := ethrpc.NewServer()
	t.Cleanup(server.Stop)
	require.NoError(t, server.RegisterName("test", failingService{}))
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	client, err := ethrpc.DialHTTP(httpServer.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

// TestErrorCodeOnTheWire requires each constructor's code and data to reach a JSON-RPC client.
func TestErrorCodeOnTheWire(t *testing.T) {
	client := dial(t)
	for _, tc := range []struct {
		method   string
		wantCode int
		wantData any
	}{
		{method: "test_serverError", wantCode: rpcerr.CodeServerError},
		{method: "test_invalidParams", wantCode: rpcerr.CodeInvalidParams},
		{method: "test_limitExceeded", wantCode: rpcerr.CodeLimitExceeded},
		{method: "test_reverted", wantCode: rpcerr.CodeExecutionReverted, wantData: hexutil.Encode([]byte{0xde, 0xad})},
		// The server reads the code of the error a method returns, not of one it wraps.
		{method: "test_wrapped", wantCode: rpcerr.CodeServerError},
	} {
		t.Run(tc.method, func(t *testing.T) {
			err := client.CallContext(t.Context(), nil, tc.method)
			var rpcErr ethrpc.Error
			require.ErrorAs(t, err, &rpcErr)
			require.Equal(t, tc.wantCode, rpcErr.ErrorCode())
			var dataErr ethrpc.DataError
			require.ErrorAs(t, err, &dataErr)
			require.Equal(t, tc.wantData, dataErr.ErrorData())
		})
	}
}

func TestErrorUnwrapsToItsCause(t *testing.T) {
	require.ErrorIs(t, rpcerr.InvalidParams(errCause), errCause)
	require.ErrorIs(t, rpcerr.LimitExceeded(errCause), errCause)
	require.ErrorIs(t, rpcerr.ServerError(errCause), errCause)
}
