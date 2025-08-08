package tests

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestGetBlockByHash(t *testing.T) {
	txBz := signAndEncodeTx(send(0), mnemonic1)
	SetupTestServer([][][]byte{{txBz}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "getBlockByHash", common.HexToHash("0x6f2168eb453152b1f68874fe32cea6fcb199bfd63836acb72a8eb33e666613fe").Hex(), true)
			fmt.Println(res)
			blockHash := res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0x6f2168eb453152b1f68874fe32cea6fcb199bfd63836acb72a8eb33e666613fe", blockHash.(string))
		},
	)
}

func TestGetSeiBlockByHash(t *testing.T) {
	cw20 := "sei18cszlvm6pze0x9sz32qnjq4vtd45xehqs8dq7cwy8yhq35wfnn3quh5sau" // hardcoded
	tx1 := signAndEncodeTx(registerCW20Pointer(0, cw20), mnemonic1)
	tx2 := signAndEncodeCosmosTx(transferCW20Msg(mnemonic1, cw20), mnemonic1, 7, 0)
	SetupTestServer([][][]byte{{tx1}, {tx2}}, mnemonicInitializer(mnemonic1), cw20Initializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("sei", port, "getBlockByHash", common.HexToHash("0x9dd3e6c427b6936f973b240cd5780b8ee4bf8fab0c8d281afb28089db51bb4af").Hex(), true)
			txs := res["result"].(map[string]interface{})["transactions"]
			require.Len(t, txs.([]interface{}), 1)
		},
	)
}

func TestGetBlockByNumber(t *testing.T) {
	txBz1 := signAndEncodeTx(send(0), mnemonic1)
	txBz2 := signAndEncodeTx(send(1), mnemonic1)
	txBz3 := signAndEncodeTx(send(2), mnemonic1)
	SetupTestServer([][][]byte{{txBz1}, {txBz2}, {txBz3}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "getBlockByNumber", "earliest", true)
			blockHash := res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0xF9D3845DF25B43B1C6926F3CEDA6845C17F5624E12212FD8847D0BA01DA1AB9E", blockHash.(string))
			res = sendRequestWithNamespace("eth", port, "getBlockByNumber", "safe", true)
			blockHash = res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0x8ace0b4e9ced0ef792034128d37eb19b9b2b06bf016d51d533216a9afd7c0e8f", blockHash.(string))
			res = sendRequestWithNamespace("eth", port, "getBlockByNumber", "latest", true)
			blockHash = res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0x8ace0b4e9ced0ef792034128d37eb19b9b2b06bf016d51d533216a9afd7c0e8f", blockHash.(string))
			res = sendRequestWithNamespace("eth", port, "getBlockByNumber", "finalized", true)
			blockHash = res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0x8ace0b4e9ced0ef792034128d37eb19b9b2b06bf016d51d533216a9afd7c0e8f", blockHash.(string))
			res = sendRequestWithNamespace("eth", port, "getBlockByNumber", "pending", true)
			blockHash = res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0x8ace0b4e9ced0ef792034128d37eb19b9b2b06bf016d51d533216a9afd7c0e8f", blockHash.(string))
			res = sendRequestWithNamespace("eth", port, "getBlockByNumber", "0x2", true)
			blockHash = res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0x6f2168eb453152b1f68874fe32cea6fcb199bfd63836acb72a8eb33e666613fe", blockHash.(string))
		},
	)
}

func TestGetBlockSkipTxIndex(t *testing.T) {
	tx1 := signAndEncodeCosmosTx(bankSendMsg(mnemonic1), mnemonic1, 7, 0)
	tx2 := signAndEncodeTx(send(0), mnemonic1)
	SetupTestServer([][][]byte{{tx1, tx2}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "getBlockByHash", common.HexToHash("0x6f2168eb453152b1f68874fe32cea6fcb199bfd63836acb72a8eb33e666613fe").Hex(), true)
			txs := res["result"].(map[string]any)["transactions"].([]any)
			require.Len(t, txs, 1)
			require.Equal(t, "0x0", txs[0].(map[string]any)["transactionIndex"].(string))
		},
	)
}
