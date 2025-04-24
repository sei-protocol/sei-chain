package tests

import (
	"encoding/json"
	"testing"

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

func TestTraceHistoricalPrecompiles(t *testing.T) {
	from := getAddrWithMnemonic(mnemonic1)
	txData := jsonExtractAsBytesFromArray(0).(*ethtypes.DynamicFeeTx)
	SetupTestServer([][][]byte{{}, {}, {}}, mnemonicInitializer(mnemonic1), mockUpgrade("v5.5.2", 1), mockUpgrade("v6.0.5", 3)).Run(
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
