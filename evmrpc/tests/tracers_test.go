package tests

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app"

	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/lib/ethapi"
	"github.com/stretchr/testify/require"
)

func TestTraceBlockByNumber(t *testing.T) {
	txBz := signAndEncodeTx(send(0), mnemonic1)
	SetupTestServer([][][]byte{{txBz}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("debug", port, "traceBlockByNumber", "0x2", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			blockHash := res["result"].([]interface{})[0].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["blockHash"]
			// assert that the block hash has been overwritten instead of the RLP hash.
			require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", blockHash.(string))
		},
	)
}

func TestTraceBlockByNumberExcludeTraceFail(t *testing.T) {
	txBz := signAndEncodeTx(send(0), mnemonic1)
	panicTxBz := signAndEncodeTx(send(100), mnemonic1)
	SetupTestServer([][][]byte{{txBz, panicTxBz}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("sei", port, "traceBlockByNumberExcludeTraceFail", "0x2", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			fmt.Println(res)
			txs := res["result"].([]interface{})
			require.Len(t, txs, 1)
			blockHash := txs[0].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["blockHash"]
			// assert that the block hash has been overwritten instead of the RLP hash.
			require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", blockHash.(string))
		},
	)
}

func TestTraceBlockByHash(t *testing.T) {
	txBz := signAndEncodeTx(send(0), mnemonic1)
	SetupTestServer([][][]byte{{txBz}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("debug", port, "traceBlockByHash", "0x0000000000000000000000000000000000000000000000000000000000000002", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			blockHash := res["result"].([]interface{})[0].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["blockHash"]
			// assert that the block hash has been overwritten instead of the RLP hash.
			require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", blockHash.(string))
		},
	)
}

func TestTraceBlockByHashExcludeTraceFail(t *testing.T) {
	txBz := signAndEncodeTx(send(0), mnemonic1)
	panicTxBz := signAndEncodeTx(send(100), mnemonic1)
	SetupTestServer([][][]byte{{txBz, panicTxBz}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("sei", port, "traceBlockByHashExcludeTraceFail", "0x0000000000000000000000000000000000000000000000000000000000000002", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			fmt.Println(res)
			txs := res["result"].([]interface{})
			require.Len(t, txs, 1)
			blockHash := txs[0].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["blockHash"]
			// assert that the block hash has been overwritten instead of the RLP hash.
			require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", blockHash.(string))
		},
	)
}

func TestTraceHistoricalPrecompiles(t *testing.T) {
	from := getAddrWithMnemonic(mnemonic1)
	txData := jsonExtractAsBytesFromArray(0).(*ethtypes.DynamicFeeTx)
	SetupTestServer([][][]byte{{}, {}, {}}, mnemonicInitializer(mnemonic1), mockUpgrade("v5.5.2", 1), mockUpgrade(app.LatestUpgrade, 3)).Run(
		func(port int) {
			args := ethapi.TransactionArgs{
				From:     &from,
				To:       txData.To,
				Gas:      (*hexutil.Uint64)(&txData.Gas),
				GasPrice: (*hexutil.Big)(txData.GasFeeCap),
				Nonce:    (*hexutil.Uint64)(&txData.Nonce),
				Input:    (*hexutil.Bytes)(&txData.Data),
				ChainID:  (*hexutil.Big)(txData.ChainID),
			}
			bz, err := json.Marshal(args)
			require.Nil(t, err)
			// error when traced on a block prior to v6.0.5
			res := sendRequestWithNamespace("debug", port, "traceCall", bz, "0x2", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			errMsg := res["result"].([]interface{})[0].(map[string]interface{})["error"].(string)
			require.Contains(t, errMsg, "no method with id")
			// no error when traced on a block post v6.0.5
			res = sendRequestWithNamespace("debug", port, "traceCall", bz, "0x3", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			resultMap := res["result"].([]interface{})[0].(map[string]interface{})
			require.NotContains(t, resultMap, "error")
		},
	)
}

func TestTraceMultipleTransactionsShouldNotHang(t *testing.T) {
	cwIter := "sei18cszlvm6pze0x9sz32qnjq4vtd45xehqs8dq7cwy8yhq35wfnn3quh5sau" // hardcoded
	txBzList := make([][]byte, 100)
	for nonce := 1; nonce <= 100; nonce++ {
		txBzList[nonce-1] = signAndEncodeTx(sendErc20(uint64(nonce)), erc20DeployerMnemonics)
	}
	txBzList = append(txBzList, signAndEncodeTx(callWasmIter(0, cwIter), mnemonic1))
	SetupTestServer([][][]byte{txBzList}, mnemonicInitializer(mnemonic1), multiCoinInitializer(mnemonic1), cwIterInitializer(mnemonic1), erc20Initializer()).Run(
		func(port int) {
			res := sendRequestWithNamespace("debug", port, "traceBlockByHash", "0x0000000000000000000000000000000000000000000000000000000000000002", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			blockHash := res["result"].([]interface{})[0].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["blockHash"]
			// assert that the block hash has been overwritten instead of the RLP hash.
			require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", blockHash.(string))
		},
	)
}

func TestTraceBlockByNumberPerformance(t *testing.T) {
	blocks := make([][][]byte, 10)
	for i := range blocks {
		blocks[i] = [][]byte{signAndEncodeTx(send(uint64(i)), mnemonic1)}
	}
	SetupTestServer(blocks, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			done := make(chan struct{})
			go func() {
				defer func() { done <- struct{}{} }()
				for i := range blocks {
					_ = sendRequestWithNamespace("debug", port, "traceBlockByNumber", fmt.Sprintf("0x%d", i+2), map[string]interface{}{
						"timeout": "60s", "tracer": "flatCallTracer",
					})
				}
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("did not finish tracing 10 blocks after 5 sec.")
			}
		},
	)
}
