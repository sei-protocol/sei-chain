package tests

import (
	"bytes"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	"github.com/stretchr/testify/require"
)

// failedTxTracers are the tracer configurations a block transaction that fails before
// execution is traced with.
var failedTxTracers = map[string]map[string]interface{}{
	"structLogger":            {},
	"callTracer":              {"tracer": "callTracer"},
	"flatCallTracer":          {"tracer": "flatCallTracer"},
	"prestateTracer":          {"tracer": "prestateTracer"},
	"prestateTracer diffMode": {"tracer": "prestateTracer", "tracerConfig": map[string]interface{}{"diffMode": true}},
	"4byteTracer":             {"tracer": "4byteTracer"},
	"noopTracer":              {"tracer": "noopTracer"},
	"muxTracer": {"tracer": "muxTracer", "tracerConfig": map[string]interface{}{
		"callTracer":     map[string]interface{}{},
		"prestateTracer": map[string]interface{}{"diffMode": true},
		"4byteTracer":    map[string]interface{}{},
	}},
}

// jsTracer is a JS tracer configuration that reports the top-level call of a
// transaction, with the nonce and balance of its sender after it.
var jsTracer = map[string]interface{}{
	"tracer": `{result: function(ctx, db) { return {type: ctx.type, error: ctx.error, gasUsed: ctx.gasUsed, nonce: db.getNonce(ctx.from), balance: db.getBalance(ctx.from).toString()}; }, fault: function() {}}`,
}

// TestTraceTxFailingBeforeExecution traces a block whose first transaction fails before
// execution, followed by a transaction of the same sender. The chain keeps the failed
// transaction with its sender's nonce bumped, so every tracer has to report it as a
// failed transaction and trace the next one on top of the bumped nonce.
func TestTraceTxFailingBeforeExecution(t *testing.T) {
	sender := getAddrWithMnemonic(mnemonic1)
	recipient := common.HexToAddress("0x00000000000000000000000000000000000a0001")
	for _, tc := range []struct {
		name    string
		failed  *ethtypes.DynamicFeeTx
		txErr   string
		gasUsed uint64
	}{
		{
			// Fails in the ante handler, which charges nothing.
			name:   "fee cap below base fee",
			failed: &ethtypes.DynamicFeeTx{Nonce: 0, GasFeeCap: big.NewInt(999999999), Gas: 21000, To: &recipient, Value: big.NewInt(2000), ChainID: chainId},
			txErr:  "max fee per gas less than block base fee",
		},
		{
			// Fails after the ante handler charged the gas limit: 100 non-zero bytes of
			// calldata cost 22600 gas of intrinsic gas, but 25000 gas of EIP-7623 floor.
			name:    "gas limit below floor data gas",
			failed:  &ethtypes.DynamicFeeTx{Nonce: 0, GasFeeCap: big.NewInt(1000000000), Gas: 22600, To: &recipient, Value: big.NewInt(2000), Data: bytes.Repeat([]byte{1}, 100), ChainID: chainId},
			txErr:   "insufficient gas for floor data gas cost: have 22600, want 25000",
			gasUsed: 22600,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := &ethtypes.DynamicFeeTx{Nonce: 1, GasFeeCap: big.NewInt(1000000000), Gas: 21000, To: &recipient, Value: big.NewInt(2000), ChainID: chainId}
			failedTx := signTxWithMnemonic(tc.failed, mnemonic1)
			nextTx := signTxWithMnemonic(next, mnemonic1)
			blocks := [][][]byte{{encodeEvmTx(tc.failed, failedTx), encodeEvmTx(next, nextTx)}}
			SetupTestServer(t, blocks, mnemonicInitializer(mnemonic1)).Run(func(port int) {
				receipt := sendRequestWithNamespace("eth", port, "getTransactionReceipt", failedTx.Hash().Hex())["result"].(map[string]interface{})
				require.Equal(t, "0x0", receipt["status"])
				require.Equal(t, hexutil.Uint64(tc.gasUsed).String(), receipt["gasUsed"])
				nextReceipt := sendRequestWithNamespace("eth", port, "getTransactionReceipt", nextTx.Hash().Hex())["result"].(map[string]interface{})
				require.Equal(t, "0x1", nextReceipt["status"])
				require.Equal(t, "0x2", sendRequestWithNamespace("eth", port, "getTransactionCount", sender.Hex(), "0x2")["result"])

				for name, config := range failedTxTracers {
					requireBlockTraceMatchesTxTraces(t, port, name, config, failedTx, nextTx)
				}

				trace := func(config map[string]interface{}) map[string]interface{} {
					return sendRequestWithNamespace("debug", port, "traceTransaction", failedTx.Hash().Hex(), config)["result"].(map[string]interface{})
				}
				structLog := trace(failedTxTracers["structLogger"])
				require.Equal(t, true, structLog["failed"])
				require.Equal(t, float64(tc.gasUsed), structLog["gas"])
				call := trace(failedTxTracers["callTracer"])
				require.Equal(t, hexutil.Uint64(tc.gasUsed).String(), call["gasUsed"])
				require.Contains(t, call["error"], tc.txErr)

				// The diff matches what the chain did to the sender: the nonce is bumped,
				// and the balance drops by the gas the receipt charges.
				diff := trace(failedTxTracers["prestateTracer diffMode"])
				senderKey := strings.ToLower(sender.Hex())
				pre := diff["pre"].(map[string]interface{})[senderKey].(map[string]interface{})
				post := diff["post"].(map[string]interface{})[senderKey].(map[string]interface{})
				require.Nil(t, pre["nonce"])
				require.Equal(t, float64(1), post["nonce"])
				price, err := hexutil.DecodeBig(receipt["effectiveGasPrice"].(string))
				require.NoError(t, err)
				charged := new(big.Int).Mul(new(big.Int).SetUint64(tc.gasUsed), price)
				if charged.Sign() == 0 {
					require.Nil(t, post["balance"])
				} else {
					preBalance, err := hexutil.DecodeBig(pre["balance"].(string))
					require.NoError(t, err)
					postBalance, err := hexutil.DecodeBig(post["balance"].(string))
					require.NoError(t, err)
					require.Equal(t, charged, new(big.Int).Sub(preBalance, postBalance))
				}
			})

			parallelized := func(cfg *evmrpcconfig.Config) { cfg.EnableParallelizedBlockTrace = true }
			SetupTestServerWithConfig(t, parallelized, blocks, mnemonicInitializer(mnemonic1)).Run(func(port int) {
				requireBlockTraceMatchesTxTraces(t, port, "parallelized structLogger", failedTxTracers["structLogger"], failedTx, nextTx)
			})

			allowJS := func(cfg *evmrpcconfig.Config) { cfg.TraceAllowJSTracers = true }
			SetupTestServerWithConfig(t, allowJS, blocks, mnemonicInitializer(mnemonic1)).Run(func(port int) {
				requireBlockTraceMatchesTxTraces(t, port, "js", jsTracer, failedTx, nextTx)
			})
		})
	}
}

// requireBlockTraceMatchesTxTraces requires the trace of block 2, which holds txs, to
// trace every transaction without error and as tracing it alone does.
func requireBlockTraceMatchesTxTraces(t *testing.T, port int, name string, config map[string]interface{}, txs ...*ethtypes.Transaction) {
	t.Helper()
	res := sendRequestWithNamespace("debug", port, "traceBlockByNumber", "0x2", config)
	require.Nil(t, res["error"], name)
	results := res["result"].([]interface{})
	require.Len(t, results, len(txs), name)
	for i, tx := range txs {
		blockResult := results[i].(map[string]interface{})
		require.Equal(t, tx.Hash().Hex(), blockResult["txHash"], name)
		require.Nil(t, blockResult["error"], "%s: transaction %d", name, i)
		single := sendRequestWithNamespace("debug", port, "traceTransaction", tx.Hash().Hex(), config)
		require.Nil(t, single["error"], "%s: transaction %d", name, i)
		require.Equal(t, single["result"], blockResult["result"], "%s: transaction %d", name, i)
	}
}
