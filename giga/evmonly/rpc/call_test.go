package rpc

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/export"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

// revertData ABI-encodes reason the same way a Solidity `require(false, reason)`
// would, i.e. the Error(string) selector followed by the encoded string.
func revertData(reason string) []byte {
	selector := crypto.Keccak256([]byte("Error(string)"))[:4]
	data := []byte(reason)
	offset := make([]byte, 32)
	offset[31] = 32
	length := make([]byte, 32)
	length[31] = byte(len(data)) //nolint:gosec // test-only, reason is short.
	padded := make([]byte, ((len(data)+31)/32)*32)
	copy(padded, data)
	out := append(append([]byte{}, selector...), offset...)
	out = append(out, length...)
	return append(out, padded...)
}

func TestCallHappyPath(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	from := common.HexToAddress("0x2000000000000000000000000000000000000002")
	var gotMsg *core.Message
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		call: func(_ context.Context, msg *core.Message) (*core.ExecutionResult, error) {
			gotMsg = msg
			return &core.ExecutionResult{ReturnData: []byte{0x2a}}, nil
		},
	}
	api := &callAPI{backend: backend}
	args := export.TransactionArgs{From: &from, To: &to}

	got, err := api.Call(t.Context(), args, ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber))

	require.NoError(t, err)
	require.Equal(t, hexutil.Bytes{0x2a}, got)
	require.NotNil(t, gotMsg)
	require.Equal(t, from, gotMsg.From)
	require.Equal(t, &to, gotMsg.To)
	require.True(t, gotMsg.SkipNonceChecks)
	require.True(t, gotMsg.SkipFromEOACheck)
	require.Equal(t, uint64(defaultCallGasCap), gotMsg.GasLimit)
}

func TestCallCapsExplicitGasAboveDefault(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	var gotMsg *core.Message
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		call: func(_ context.Context, msg *core.Message) (*core.ExecutionResult, error) {
			gotMsg = msg
			return &core.ExecutionResult{}, nil
		},
	}
	api := &callAPI{backend: backend}
	requested := hexutil.Uint64(defaultCallGasCap * 10)
	args := export.TransactionArgs{To: &to, Gas: &requested}

	_, err := api.Call(t.Context(), args, ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber))

	require.NoError(t, err)
	require.Equal(t, uint64(defaultCallGasCap), gotMsg.GasLimit)
}

func TestCallPreservesExplicitGasBelowDefault(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	var gotMsg *core.Message
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		call: func(_ context.Context, msg *core.Message) (*core.ExecutionResult, error) {
			gotMsg = msg
			return &core.ExecutionResult{}, nil
		},
	}
	api := &callAPI{backend: backend}
	requested := hexutil.Uint64(21_000)
	args := export.TransactionArgs{To: &to, Gas: &requested}

	_, err := api.Call(t.Context(), args, ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber))

	require.NoError(t, err)
	require.Equal(t, uint64(21_000), gotMsg.GasLimit)
}

func TestCallSurfacesRevertReason(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	revert := revertData("insufficient balance")
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		call: func(context.Context, *core.Message) (*core.ExecutionResult, error) {
			return &core.ExecutionResult{Err: vm.ErrExecutionReverted, ReturnData: revert}, nil
		},
	}
	api := &callAPI{backend: backend}

	got, err := api.Call(t.Context(), export.TransactionArgs{To: &to}, ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber))

	require.Nil(t, got)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient balance")
	var revertErr *revertError
	require.ErrorAs(t, err, &revertErr)
	require.Equal(t, 3, revertErr.ErrorCode())
	require.Equal(t, hexutil.Encode(revert), revertErr.ErrorData())
}

func TestCallPassesThroughNonRevertExecutionError(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	wantErr := errors.New("out of gas")
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		call: func(context.Context, *core.Message) (*core.ExecutionResult, error) {
			return &core.ExecutionResult{Err: wantErr}, nil
		},
	}
	api := &callAPI{backend: backend}

	got, err := api.Call(t.Context(), export.TransactionArgs{To: &to}, ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber))

	require.Nil(t, got)
	require.ErrorIs(t, err, wantErr)
}

func TestCallRejectsHistoricalState(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend := &testBackend{
		call: func(context.Context, *core.Message) (*core.ExecutionResult, error) {
			t.Fatal("historical call request reached the backend")
			return nil, nil
		},
	}
	api := &callAPI{backend: backend}

	for _, block := range []ethrpc.BlockNumberOrHash{
		ethrpc.BlockNumberOrHashWithNumber(ethrpc.EarliestBlockNumber),
		ethrpc.BlockNumberOrHashWithNumber(7),
		ethrpc.BlockNumberOrHashWithHash(common.Hash{2}, true),
	} {
		got, err := api.Call(t.Context(), export.TransactionArgs{To: &to}, block)
		require.ErrorIs(t, err, errHistoricalStateUnsupported)
		require.Nil(t, got)
	}
}

func TestHandlerServesCall(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		call: func(context.Context, *core.Message) (*core.ExecutionResult, error) {
			return &core.ExecutionResult{ReturnData: []byte{0x01, 0x02}}, nil
		},
	}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore())
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got hexutil.Bytes
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_call", map[string]any{"to": to}, "latest"))
	require.Equal(t, hexutil.Bytes{0x01, 0x02}, got)
}
