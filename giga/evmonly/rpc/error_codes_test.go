package rpc

import (
	"context"
	"fmt"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/rpcerr"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// wireCase is one request and the JSON-RPC error code its client must receive.
type wireCase struct {
	name     string
	method   string
	params   []any
	wantCode int
}

// requireWireCodes sends each case's request through client and requires the code it receives.
func requireWireCodes(t *testing.T, client *ethrpc.Client, cases []wireCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got any
			requireErrorCode(t, client.CallContext(t.Context(), &got, tc.method, tc.params...), tc.wantCode)
		})
	}
}

// dialStore serves backend and store over HTTP under limits and returns a client for it.
func dialStore(t *testing.T, backend Backend, store receiptdb.ReceiptStore, limits gigaconfig.RPCConfig) *ethrpc.Client {
	t.Helper()
	handler, err := newHandler(backend, store, limits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func TestLogErrorCodesOnTheWire(t *testing.T) {
	// Receipts are indexed through block 9, the committed head is 12, and the node holds block 13.
	store := filterFixtureStore(t)
	require.NoError(t, store.PruneHistory(6))
	limits := noRPCLimits
	limits.MaxFilters = 2
	backend := filterFixtureBackend(t, 13)
	backend.blockNumber = func() uint64 { return 12 }
	client := dialStore(t, backend, store, limits)

	var blockFilter string
	require.NoError(t, client.CallContext(t.Context(), &blockFilter, "eth_newBlockFilter"))
	var logFilter string
	require.NoError(t, client.CallContext(t.Context(), &logFilter, "eth_newFilter", map[string]any{"fromBlock": "0x7"}))
	unknownHash := common.HexToHash("0xdead")

	requireWireCodes(t, client, []wireCase{
		{name: "toBlock above the head", method: "eth_getLogs", params: []any{map[string]any{"fromBlock": "0x7", "toBlock": "0xd"}}, wantCode: rpcerr.CodeInvalidParams},
		{name: "fromBlock above the head", method: "eth_getLogs", params: []any{map[string]any{"fromBlock": "0xd", "toBlock": "0xe"}}, wantCode: rpcerr.CodeInvalidParams},
		{name: "fromBlock at the head before it is indexed", method: "eth_getLogs", params: []any{map[string]any{"fromBlock": "0xc"}}, wantCode: rpcerr.CodeResourceUnavailable},
		{name: "fromBlock not indexed yet", method: "eth_getLogs", params: []any{map[string]any{"fromBlock": "0xa", "toBlock": "latest"}}, wantCode: rpcerr.CodeResourceUnavailable},
		{name: "fromBlock after an explicit toBlock", method: "eth_getLogs", params: []any{map[string]any{"fromBlock": "0xa", "toBlock": "0x9"}}, wantCode: rpcerr.CodeInvalidParams},
		{name: "executed block not indexed yet", method: "eth_getLogs", params: []any{map[string]any{"blockHash": filterBlockHash(11)}}, wantCode: rpcerr.CodeResourceUnavailable},
		{name: "held block not executed yet", method: "eth_getLogs", params: []any{map[string]any{"blockHash": filterBlockHash(13)}}, wantCode: rpcerr.CodeResourceUnavailable},
		{name: "pruned fromBlock and toBlock above the head", method: "eth_getLogs", params: []any{map[string]any{"fromBlock": "0x5", "toBlock": "0xd"}}, wantCode: rpcerr.CodeResourceUnavailable},
		{name: "toBlock not indexed yet", method: "eth_getLogs", params: []any{map[string]any{"fromBlock": "0x7", "toBlock": "0xb"}}, wantCode: rpcerr.CodeResourceUnavailable},
		{name: "pruned fromBlock", method: "eth_getLogs", params: []any{map[string]any{"fromBlock": "0x5", "toBlock": "0x8"}}, wantCode: rpcerr.CodeResourceUnavailable},
		{name: "unknown blockHash", method: "eth_getLogs", params: []any{map[string]any{"blockHash": unknownHash}}, wantCode: rpcerr.CodeResourceNotFound},
		{name: "filter with a pruned fromBlock", method: "eth_newFilter", params: []any{map[string]any{"fromBlock": "0x5"}}, wantCode: rpcerr.CodeResourceUnavailable},
		{name: "unknown filter", method: "eth_getFilterChanges", params: []any{"0x1"}, wantCode: rpcerr.CodeResourceNotFound},
		{name: "logs of a block filter", method: "eth_getFilterLogs", params: []any{blockFilter}, wantCode: rpcerr.CodeResourceNotFound},
		{name: "filter over the limit", method: "eth_newBlockFilter", wantCode: rpcerr.CodeLimitExceeded},
	})

	wide := filterFixtureStore(t)
	require.NoError(t, wide.SetLatestVersion(5000))
	requireWireCodes(t, dialStore(t, filterFixtureBackend(t, 5000), wide, noRPCLimits), []wireCase{
		{name: "range too wide", method: "eth_getLogs", params: []any{map[string]any{"fromBlock": "0x1", "toBlock": "0x7d1"}}, wantCode: rpcerr.CodeLimitExceeded},
	})
}

func TestBlockAndStateErrorCodesOnTheWire(t *testing.T) {
	backend := pastBlocksBackend(5)
	block := backend.block
	backend.block = func(ctx context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		if req.Height != nil && *req.Height == 1 {
			return nil, coretypes.WrapErrHeightNotAvailable(1, utils.Some(int64(2)))
		}
		return block(ctx, req)
	}
	backend.minGasPrice = func() *big.Int { return big.NewInt(1) }
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetLatestVersion(5))
	client := dialStore(t, backend, store, gigaconfig.RPCConfig{
		MaxRequestBodyBytes:    testRequestBodyBytes,
		MaxConcurrentSendCalls: 1,
		MaxEstimateGasCalls:    1,
	})
	address := common.Address{1}
	unknownHash := common.HexToHash("0xdead")

	requireWireCodes(t, client, []wireCase{
		{name: "pruned block", method: "eth_getBlockByNumber", params: []any{"0x1", false}, wantCode: rpcerr.CodeResourceUnavailable},
		{name: "state at an unknown block hash", method: "eth_getBalance", params: []any{address, map[string]any{"blockHash": unknownHash}}, wantCode: rpcerr.CodeResourceNotFound},
		{name: "storage key not hex", method: "eth_getStorageAt", params: []any{address, "0xzz", "latest"}, wantCode: rpcerr.CodeInvalidParams},
		{name: "reward percentiles out of order", method: "eth_feeHistory", params: []any{"0x1", "latest", []float64{50, 10}}, wantCode: rpcerr.CodeInvalidParams},
		{name: "fee history past the indexed head", method: "eth_feeHistory", params: []any{"0x1", "0x9", []float64{}}, wantCode: rpcerr.CodeResourceUnavailable},
		{name: "pending nonce without a mempool", method: "eth_getTransactionCount", params: []any{address, "pending"}, wantCode: rpcerr.CodeResourceUnavailable},
		{name: "VM error of an unknown transaction", method: "eth_getVMError", params: []any{unknownHash}, wantCode: rpcerr.CodeResourceNotFound},
		{name: "too many calls to estimate after", method: "eth_estimateGasAfterCalls", params: []any{map[string]any{}, []any{map[string]any{}, map[string]any{}}}, wantCode: rpcerr.CodeLimitExceeded},
	})
}

func TestSendErrorCodesOnTheWire(t *testing.T) {
	_, raw := testSignedTransaction(t)
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	unprotected, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{Gas: 21_000, To: &to}), ethtypes.HomesteadSigner{}, key)
	require.NoError(t, err)
	rawUnprotected, err := unprotected.MarshalBinary()
	require.NoError(t, err)
	foreign, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: big.NewInt(0x12345), Gas: 21_000, To: &to}),
		ethtypes.LatestSignerForChainID(big.NewInt(0x12345)), key)
	require.NoError(t, err)
	rawForeign, err := foreign.MarshalBinary()
	require.NoError(t, err)

	for _, tc := range []struct {
		name     string
		raw      []byte
		result   *coretypes.ResultBroadcastTx
		err      error
		wantCode int
	}{
		{name: "not replay-protected", raw: rawUnprotected, wantCode: rpcerr.CodeTransactionRejected},
		{name: "signed for another chain", raw: rawForeign, wantCode: rpcerr.CodeTransactionRejected},
		{name: "refused by CheckTx", raw: raw, result: &coretypes.ResultBroadcastTx{Code: 1, Log: "insufficient funds"}, wantCode: rpcerr.CodeTransactionRejected},
		{name: "already known", raw: raw, err: abevm.ErrTxAlreadyKnown, wantCode: rpcerr.CodeTransactionRejected},
		{name: "bad nonce", raw: raw, err: fmt.Errorf("%w: got 1, want 0", abevm.ErrTxBadNonce), wantCode: rpcerr.CodeTransactionRejected},
		{name: "too large", raw: raw, err: abevm.ErrTxTooLarge, wantCode: rpcerr.CodeTransactionRejected},
		{name: "not producing", raw: raw, err: abevm.ErrNotProducing, wantCode: rpcerr.CodeResourceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &testBackend{
				broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
					return tc.result, tc.err
				},
				proxy: utils.None[*ethrpc.Client](),
			}
			client := dialHandler(t, backend, noRPCLimits)
			var hash common.Hash
			err := client.CallContext(t.Context(), &hash, "eth_sendRawTransaction", hexutil.Bytes(tc.raw))
			requireErrorCode(t, err, tc.wantCode)
		})
	}
}

// A proxied transaction's refusal reaches the client with the code the shard owner answered with.
func TestSendPassesProxiedErrorCodeThrough(t *testing.T) {
	_, raw := testSignedTransaction(t)
	remote := dialHandler(t, &testBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			return nil, abevm.ErrTxAlreadyKnown
		},
		proxy: utils.None[*ethrpc.Client](),
	}, noRPCLimits)
	client := dialHandler(t, &testBackend{proxy: utils.Some(remote)}, noRPCLimits)
	var hash common.Hash
	err := client.CallContext(t.Context(), &hash, "eth_sendRawTransaction", hexutil.Bytes(raw))
	requireErrorCode(t, err, rpcerr.CodeTransactionRejected)
}
