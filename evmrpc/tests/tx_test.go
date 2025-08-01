package tests

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetTransactionSkipSyntheticIndex(t *testing.T) {
	tx1 := signAndEncodeCosmosTx(bankSendMsg(mnemonic1), mnemonic1, 7, 0)
	tx2Data := send(0)
	signedTx2 := signTxWithMnemonic(tx2Data, mnemonic1)
	tx2 := encodeEvmTx(tx2Data, signedTx2)
	SetupTestServer([][][]byte{{tx1, tx2}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "getTransactionByHash", signedTx2.Hash().Hex())
			txIdx := res["result"].(map[string]any)["transactionIndex"].(string)
			require.Equal(t, "0x0", txIdx) // should skip the first tx as it's not EVM
		},
	)
}

func TestGetTransactionAnteFailed(t *testing.T) {
	tx1Data := send(1) // incorrect nonce
	signedTx1 := signTxWithMnemonic(tx1Data, mnemonic1)
	tx1 := encodeEvmTx(tx1Data, signedTx1)
	SetupTestServer([][][]byte{{tx1}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "getTransactionByHash", signedTx1.Hash().Hex())
			require.Equal(t, "not found", res["error"].(map[string]interface{})["message"].(string))
		},
	)
}

func TestGetTransactionGasPrice(t *testing.T) {
	txData := send(0)
	signedTx := signTxWithMnemonic(txData, mnemonic1)
	tx := encodeEvmTx(txData, signedTx)
	SetupTestServer([][][]byte{{tx}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "getTransactionByHash", signedTx.Hash().Hex())
			result := res["result"].(map[string]any)

			// Verify gasPrice field exists and has the expected value
			gasPrice, exists := result["gasPrice"]
			require.True(t, exists, "gasPrice field should exist in response")

			// The gasPrice should match the GasFeeCap from the DynamicFeeTx
			expectedGasPrice := "0x3b9aca00" // 1000000000 in hex
			require.Equal(t, expectedGasPrice, gasPrice, "gasPrice should match the transaction's GasFeeCap")
		},
	)
}
