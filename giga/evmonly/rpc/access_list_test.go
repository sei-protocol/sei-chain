package rpc

import (
	"context"
	"errors"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/export"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

func TestCreateAccessListEndToEnd(t *testing.T) {
	from := common.HexToAddress("0x2000000000000000000000000000000000000002")
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	accessList := ethtypes.AccessList{{Address: common.Address{0x03}, StorageKeys: []common.Hash{{0x04}}}}
	for _, tc := range []struct {
		name    string
		result  *core.ExecutionResult
		backErr error
		block   any
		want    map[string]any
		wantErr string
	}{
		{
			name:   "successful execution",
			result: &core.ExecutionResult{UsedGas: 30_000},
			want: map[string]any{
				"accessList": []any{map[string]any{"address": "0x0300000000000000000000000000000000000000",
					"storageKeys": []any{"0x0400000000000000000000000000000000000000000000000000000000000000"}}},
				"gasUsed": "0x7530",
			},
		},
		{
			name:   "reverted execution reports its error",
			result: &core.ExecutionResult{UsedGas: 25_000, Err: vm.ErrExecutionReverted},
			want: map[string]any{
				"accessList": []any{map[string]any{"address": "0x0300000000000000000000000000000000000000",
					"storageKeys": []any{"0x0400000000000000000000000000000000000000000000000000000000000000"}}},
				"gasUsed": "0x61a8",
				"error":   "execution reverted",
			},
		},
		{name: "backend failure", backErr: errors.New("boom"), wantErr: "boom"},
		{name: "past block", block: "0x1", wantErr: errHistoricalStateUnsupported.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotMsg *core.Message
			backend := &testBackend{
				chainID: func() uint64 { return 713715 },
				baseFee: func() (*big.Int, error) { return new(big.Int), nil },
				createAccessList: func(_ context.Context, msg *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error) {
					gotMsg = msg
					return accessList, tc.result, tc.backErr
				},
			}
			client := dialHandler(t, backend, noRPCLimits)
			params := []any{map[string]any{"from": from, "to": to}}
			if tc.block != nil {
				params = append(params, tc.block)
			}

			var got map[string]any
			err := client.CallContext(t.Context(), &got, "eth_createAccessList", params...)

			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, from, gotMsg.From)
			require.Equal(t, &to, gotMsg.To)
			require.Equal(t, uint64(defaultCallGasCap), gotMsg.GasLimit)
			require.True(t, gotMsg.SkipNonceChecks)
		})
	}
}

func TestEstimateGasAfterCalls(t *testing.T) {
	from := common.HexToAddress("0x2000000000000000000000000000000000000002")
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	callTarget := common.HexToAddress("0x3000000000000000000000000000000000000003")
	for _, tc := range []struct {
		name     string
		calls    int
		maxCalls int
		block    *ethrpc.BlockNumberOrHash
		revert   bool
		wantErr  string
	}{
		{name: "calls reach the backend in order", calls: 2},
		{name: "no calls estimates like eth_estimateGas"},
		{name: "calls at the cap", calls: 3, maxCalls: 3},
		{name: "calls over the cap", calls: 4, maxCalls: 3, wantErr: "too many calls (4 > 3)"},
		{name: "revert", calls: 1, revert: true, wantErr: "execution reverted"},
		{name: "past block", calls: 1, block: func() *ethrpc.BlockNumberOrHash {
			block := ethrpc.BlockNumberOrHashWithNumber(1)
			return &block
		}(), wantErr: errHistoricalStateUnsupported.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotCalls []*core.Message
			var gotMsg *core.Message
			estimate := func(calls []*core.Message, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
				gotCalls, gotMsg = calls, msg
				require.Equal(t, uint64(defaultCallGasCap), gasCap)
				if tc.revert {
					return 0, nil, vm.ErrExecutionReverted
				}
				return 42_000, nil, nil
			}
			backend := &testBackend{
				chainID: func() uint64 { return 713715 },
				baseFee: func() (*big.Int, error) { return new(big.Int), nil },
				estimateGas: func(_ context.Context, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
					return estimate(nil, msg, gasCap)
				},
				estimateGasAfterCalls: func(_ context.Context, calls []*core.Message, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
					require.NotEmpty(t, calls)
					return estimate(calls, msg, gasCap)
				},
			}
			api := &estimateAPI{backend: backend, maxCalls: tc.maxCalls}
			calls := make([]export.TransactionArgs, tc.calls)
			for i := range calls {
				value := hexutil.Big(*big.NewInt(int64(i)))
				calls[i] = export.TransactionArgs{From: &from, To: &callTarget, Value: &value}
			}
			block := latestBlockSelector()
			if tc.block != nil {
				block = tc.block
			}

			got, err := api.EstimateGasAfterCalls(t.Context(), export.TransactionArgs{From: &from, To: &to}, calls, block)

			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, hexutil.Uint64(42_000), got)
			require.Len(t, gotCalls, tc.calls)
			for i, call := range gotCalls {
				require.Equal(t, &callTarget, call.To)
				require.Equal(t, big.NewInt(int64(i)), call.Value)
				require.Equal(t, uint64(defaultCallGasCap), call.GasLimit)
			}
			require.Equal(t, &to, gotMsg.To)
			require.Zero(t, gotMsg.GasLimit, "an omitted gas reaches the estimator as 0")
		})
	}
}

func TestEstimateGasAfterCallsCapComesFromConfig(t *testing.T) {
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		baseFee: func() (*big.Int, error) { return new(big.Int), nil },
	}
	limits := noRPCLimits
	limits.MaxEstimateGasCalls = 1
	client := dialHandler(t, backend, limits)

	var got hexutil.Uint64
	err := client.CallContext(t.Context(), &got, "eth_estimateGasAfterCalls", map[string]any{}, []any{map[string]any{}, map[string]any{}})

	require.ErrorContains(t, err, "too many calls (2 > 1)")
}

// dialHandler serves backend over HTTP under limits and returns a client for it.
func dialHandler(t *testing.T, backend Backend, limits gigaconfig.RPCConfig) *ethrpc.Client {
	t.Helper()
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), limits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}
