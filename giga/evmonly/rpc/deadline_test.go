package rpc

import (
	"context"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

// errcodeRequestTimeout is the JSON-RPC error code go-ethereum reports for a method that failed
// after its deadline passed.
const errcodeRequestTimeout = -32002

// limitsWithDeadlines returns noRPCLimits with the given per-method deadlines.
func limitsWithDeadlines(defaultTimeout time.Duration, methodTimeouts ...string) gigaconfig.RPCConfig {
	limits := noRPCLimits
	limits.DefaultTimeout = defaultTimeout
	limits.MethodTimeouts = methodTimeouts
	return limits
}

// dialTransports returns a client for handler-under-limits over each transport, keyed by name.
func dialTransports(t *testing.T, backend Backend, limits gigaconfig.RPCConfig) map[string]*ethrpc.Client {
	t.Helper()
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), limits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	httpClient, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(httpClient.Close)

	wsClient, err := ethrpc.DialWebsocket(t.Context(), serveWebsocket(t, backend, limits), "")
	require.NoError(t, err)
	t.Cleanup(wsClient.Close)
	return map[string]*ethrpc.Client{"http": httpClient, "ws": wsClient}
}

// callBackend returns a backend whose eth_call runs call.
func callBackend(call func(context.Context, *core.Message) (*core.ExecutionResult, error)) *testBackend {
	return &testBackend{
		chainID: func() uint64 { return 713715 },
		baseFee: func() *big.Int { return new(big.Int) },
		call:    call,
	}
}

var deadlineTestTo = common.HexToAddress("0x1000000000000000000000000000000000000001")

func TestMethodDeadlineEndsBlockedCall(t *testing.T) {
	backend := callBackend(func(ctx context.Context, _ *core.Message) (*core.ExecutionResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	for name, client := range dialTransports(t, backend, limitsWithDeadlines(time.Hour, "eth_call=50ms")) {
		t.Run(name, func(t *testing.T) {
			var got hexutil.Bytes
			err := client.CallContext(t.Context(), &got, "eth_call", map[string]any{"to": deadlineTestTo}, "latest")

			var rpcErr ethrpc.Error
			require.ErrorAs(t, err, &rpcErr)
			require.Equal(t, errcodeRequestTimeout, rpcErr.ErrorCode())
		})
	}
}

func TestDefaultDeadlineBoundsUnlistedMethod(t *testing.T) {
	backend := callBackend(func(ctx context.Context, _ *core.Message) (*core.ExecutionResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	for name, client := range dialTransports(t, backend, limitsWithDeadlines(50*time.Millisecond, "eth_estimateGas=1h")) {
		t.Run(name, func(t *testing.T) {
			var got hexutil.Bytes
			err := client.CallContext(t.Context(), &got, "eth_call", map[string]any{"to": deadlineTestTo}, "latest")

			var rpcErr ethrpc.Error
			require.ErrorAs(t, err, &rpcErr)
			require.Equal(t, errcodeRequestTimeout, rpcErr.ErrorCode())
		})
	}
}

func TestZeroMethodDeadlineLeavesCallUnbounded(t *testing.T) {
	backend := callBackend(func(ctx context.Context, _ *core.Message) (*core.ExecutionResult, error) {
		if _, ok := ctx.Deadline(); ok {
			return nil, context.DeadlineExceeded
		}
		return &core.ExecutionResult{ReturnData: []byte{0x01}}, nil
	})
	for name, client := range dialTransports(t, backend, limitsWithDeadlines(time.Millisecond, "eth_call=0")) {
		t.Run(name, func(t *testing.T) {
			var got hexutil.Bytes
			require.NoError(t, client.CallContext(t.Context(), &got, "eth_call", map[string]any{"to": deadlineTestTo}, "latest"))
			require.Equal(t, hexutil.Bytes{0x01}, got)
		})
	}
}

func TestNewHandlerRejectsMalformedMethodTimeouts(t *testing.T) {
	_, err := newHandler(&testBackend{}, evmonly.NewMemoryReceiptStore(), limitsWithDeadlines(0, "eth_call"))
	require.ErrorContains(t, err, gigaconfig.FlagRPCMethodTimeouts)
}
