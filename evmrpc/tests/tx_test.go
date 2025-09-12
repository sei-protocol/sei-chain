package tests

import (
	"strconv"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
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

func TestGetTransactionReceiptFailedTx(t *testing.T) {
	tx1Data := sendErc20(0)
	signedTx1 := signTxWithMnemonic(tx1Data, mnemonic1)
	tx1 := encodeEvmTx(tx1Data, signedTx1)
	SetupTestServer([][][]byte{{tx1}}, erc20Initializer(), mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "getTransactionReceipt", signedTx1.Hash().Hex())
			receipt := res["result"].(map[string]interface{})
			require.Equal(t, "0x0", receipt["status"])
			require.Equal(t, erc20Addr, common.HexToAddress(receipt["to"].(string)))
			require.Equal(t, "0x2", receipt["blockNumber"])
			require.Greater(t, receipt["gasUsed"].(string), "0x0")
			require.Greater(t, receipt["effectiveGasPrice"].(string), "0x0")
		},
	)
}

// Unified test from both branches
func TestEVMTransactionIndexResponseCorrectnessAndConsistency(t *testing.T) {
	cosmosTx1 := signAndEncodeCosmosTx(bankSendMsg(mnemonic1), mnemonic1, 7, 0)
	tx1Data := send(0)
	signedTx1 := signTxWithMnemonic(tx1Data, mnemonic1)
	tx1 := encodeEvmTx(tx1Data, signedTx1)

	cosmosTx2 := signAndEncodeCosmosTx(bankSendMsg(mnemonic1), mnemonic1, 7, 1)
	tx2Data := send(1)
	signedTx2 := signTxWithMnemonic(tx2Data, mnemonic1)
	tx2 := encodeEvmTx(tx2Data, signedTx2)

	SetupTestServer([][][]byte{{cosmosTx1, tx1, cosmosTx2, tx2}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			blockNumber := "0x2"
			numberOfEVMTransactions := 2
			correctTxIndex := int64(1)
			retrievalTxIndex := "0x1"

			blockResult := sendRequestWithNamespace("eth", port, "getBlockByNumber", blockNumber, false)
			require.NotNil(t, blockResult["result"])
			blockHash := blockResult["result"].(map[string]interface{})["hash"].(string)

			txHash := signedTx2.Hash()

			receipt := sendRequestWithNamespace("eth", port, "getTransactionReceipt", txHash.Hex())["result"].(map[string]interface{})
			tx := sendRequestWithNamespace("eth", port, "getTransactionByHash", txHash.Hex())["result"].(map[string]interface{})
			txIndexFromHash := tx["transactionIndex"].(string)

			txFromBlockHashAndIndex := sendRequestWithNamespace("eth", port, "getTransactionByBlockHashAndIndex", blockHash, retrievalTxIndex)["result"].(map[string]interface{})
			txIndexFromBlockHashAndIndex := txFromBlockHashAndIndex["transactionIndex"].(string)

			txFromBlockNumberAndIndex := sendRequestWithNamespace("eth", port, "getTransactionByBlockNumberAndIndex", blockNumber, retrievalTxIndex)["result"].(map[string]interface{})
			txIndexFromBlockNumberAndIndex := txFromBlockNumberAndIndex["transactionIndex"].(string)

			blockByHash := sendRequestWithNamespace("eth", port, "getBlockByHash", blockHash, true)["result"].(map[string]interface{})
			txIndexFromBlockByHash := blockByHash["transactions"].([]interface{})[correctTxIndex].(map[string]interface{})["transactionIndex"].(string)

			blockByNumber := sendRequestWithNamespace("eth", port, "getBlockByNumber", blockNumber, true)["result"].(map[string]interface{})
			txIndexFromBlockByNumber := blockByNumber["transactions"].([]interface{})[correctTxIndex].(map[string]interface{})["transactionIndex"].(string)

			blockReceipts := sendRequestWithNamespace("eth", port, "getBlockReceipts", blockHash)["result"].([]interface{})
			var txIndexFromBlockReceipts string
			for _, receipt := range blockReceipts {
				r := receipt.(map[string]interface{})
				if r["transactionHash"] == txHash.Hex() {
					txIndexFromBlockReceipts = r["transactionIndex"].(string)
					break
				}
			}
			require.NotEmpty(t, txIndexFromBlockReceipts, "Should find transaction index in block receipts")

			allIndices := []string{
				receipt["transactionIndex"].(string),
				txIndexFromHash,
				txIndexFromBlockHashAndIndex,
				txIndexFromBlockNumberAndIndex,
				txIndexFromBlockByHash,
				txIndexFromBlockByNumber,
				txIndexFromBlockReceipts,
			}

			for i := 1; i < len(allIndices); i++ {
				actualTxIndex, err := strconv.ParseInt(allIndices[i], 0, 64)
				require.Nil(t, err)
				require.Equal(t, correctTxIndex, actualTxIndex,
					"Transaction index should be the same and correct across all endpoints. Expected: %d, Got: %d", correctTxIndex, actualTxIndex)
			}
		},
	)
}
