package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	receipttypes "github.com/sei-protocol/sei-chain/sei-db/receipt_db/types"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestHandlersPreservePrunedBlockErrors(t *testing.T) {
	blockHash := common.Hash{1}
	txHash := common.Hash{2}
	for _, tc := range []struct {
		name string
		base utils.Option[int64]
		want string
	}{
		{name: "block history", want: "height is not available (requested height: 4)"},
		{name: "receipt history", base: utils.Some[int64](6), want: "height is not available (requested height: 4, base height: 6)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pruned := &abevm.PrunedError{Height: 4, Base: tc.base}
			backend := &testBackend{
				blockNumber: func() uint64 { return 10 },
				block: func(context.Context, *coretypes.RequestBlockInfo) (*abevm.EvmBlock, error) {
					return nil, pruned
				},
				blockByHash: func(context.Context, *coretypes.RequestBlockByHash) (*abevm.EvmBlock, error) {
					return nil, pruned
				},
			}
			store := evmonly.NewMemoryReceiptStore()
			require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()).WithBlockHeight(4), []receipttypes.ReceiptRecord{{
				TxHash:  txHash,
				Receipt: &evmtypes.Receipt{TxHashHex: txHash.Hex(), BlockNumber: 4},
			}}))
			handler, err := newHandler(backend, store, noRPCLimits)
			require.NoError(t, err)
			defer handler.Stop()
			server := httptest.NewServer(handler)
			defer server.Close()
			client, err := ethrpc.DialHTTP(server.URL)
			require.NoError(t, err)
			defer client.Close()

			for i, call := range []struct {
				method string
				args   []any
				prefix string
			}{
				{method: "eth_getBlockByNumber", args: []any{"0x4", false}},
				{method: "eth_getBlockByHash", args: []any{blockHash, false}},
				{method: "eth_getBlockTransactionCountByNumber", args: []any{"0x4"}},
				{method: "eth_getBlockTransactionCountByHash", args: []any{blockHash}},
				{method: "eth_getTransactionByBlockNumberAndIndex", args: []any{"0x4", "0x0"}},
				{method: "eth_getTransactionByBlockHashAndIndex", args: []any{blockHash, "0x0"}},
				{method: "eth_getBlockReceipts", args: []any{"0x4"}},
				{method: "eth_getBlockReceipts", args: []any{blockHash}},
				{method: "eth_getLogs", args: []any{map[string]any{"blockHash": blockHash}}},
				{method: "eth_getBalance", args: []any{common.Address{}, map[string]any{"blockHash": blockHash}}},
				{method: "eth_getTransactionByHash", args: []any{txHash}, prefix: "read receipt block 4: "},
				{method: "eth_getTransactionReceipt", args: []any{txHash}, prefix: "read receipt block 4: "},
				{method: "debug_traceBlockByNumber", args: []any{"0x4", nil}},
				{method: "debug_traceBlockByHash", args: []any{blockHash, nil}},
			} {
				t.Run(fmt.Sprintf("%s/%d", call.method, i), func(t *testing.T) {
					var result json.RawMessage
					err := client.CallContext(t.Context(), &result, call.method, call.args...)
					var rpcErr ethrpc.Error
					require.ErrorAs(t, err, &rpcErr)
					require.Equal(t, -32000, rpcErr.ErrorCode())
					require.EqualError(t, err, call.prefix+tc.want)
				})
			}
		})
	}
}

func TestLoadBackendBlockTranslatesPrunedHeight(t *testing.T) {
	backend := &testBackend{
		block: func(context.Context, *coretypes.RequestBlockInfo) (*abevm.EvmBlock, error) {
			return nil, abevm.ErrPruned
		},
	}
	_, err := loadBackendBlock(t.Context(), backend, utils.Some[int64](4))
	require.ErrorIs(t, err, abevm.ErrPruned)
	require.EqualError(t, err, "height is not available (requested height: 4)")

	want := errors.New("block read failed")
	require.Same(t, want, translateBlockError(want, utils.Some[int64](4)))
}

func TestLoadBackendBlockResolvesPrunedLatestLazily(t *testing.T) {
	headReads := 0
	backend := &testBackend{
		blockNumber: func() uint64 {
			headReads++
			return 4
		},
		block: func(context.Context, *coretypes.RequestBlockInfo) (*abevm.EvmBlock, error) {
			return nil, nil
		},
	}
	_, err := loadBackendBlock(t.Context(), backend, utils.None[int64]())
	require.ErrorIs(t, err, errBlockAboveHead)
	require.Zero(t, headReads)

	backend.block = func(context.Context, *coretypes.RequestBlockInfo) (*abevm.EvmBlock, error) {
		return nil, abevm.ErrPruned
	}
	_, err = loadBackendBlock(t.Context(), backend, utils.None[int64]())
	require.EqualError(t, err, "height is not available (requested height: 4)")
	require.Equal(t, 1, headReads)
}

func TestHandlerPreservesNullForNonPositiveResolvedHead(t *testing.T) {
	backend := &testBackend{blockNumber: func() uint64 { return 0 }}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), noRPCLimits)
	require.NoError(t, err)
	defer handler.Stop()
	server := httptest.NewServer(pinHeadPerRequest(backend, handler))
	defer server.Close()
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	defer client.Close()
	var result json.RawMessage
	require.NoError(t, client.CallContext(t.Context(), &result, "eth_getBlockByNumber", "latest", false))
	require.JSONEq(t, "null", string(result))
}
