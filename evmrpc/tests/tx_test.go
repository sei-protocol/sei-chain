package tests

import (
	"strconv"
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

// Does not check trace_*, debug_*, and log endpoints
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

			blockResult := sendRequestWithNamespace("eth", port, "getBlockByNumber", blockNumber, false)
			require.NotNil(t, blockResult["result"])
			blockHash := blockResult["result"].(map[string]interface{})["hash"].(string)

			txHash := signedTx2.Hash()
			correctTxIndex := int64(1)
			retrievalTxIndex := "0x1"

			receiptResult := sendRequestWithNamespace("eth", port, "getTransactionReceipt", txHash.Hex())
			require.NotNil(t, receiptResult["result"])
			receipt := receiptResult["result"].(map[string]interface{})
			receiptTxIndex := receipt["transactionIndex"].(string)

			txResult := sendRequestWithNamespace("eth", port, "getTransactionByHash", txHash.Hex())
			require.NotNil(t, txResult["result"])
			tx := txResult["result"].(map[string]interface{})
			txIndexFromHash := tx["transactionIndex"].(string)

			blockHashAndIndexResult := sendRequestWithNamespace("eth", port, "getTransactionByBlockHashAndIndex", blockHash, retrievalTxIndex)
			require.NotNil(t, blockHashAndIndexResult["result"])
			txFromBlockHashAndIndex := blockHashAndIndexResult["result"].(map[string]interface{})
			txIndexFromBlockHashAndIndex := txFromBlockHashAndIndex["transactionIndex"].(string)

			blockNumberAndIndexResult := sendRequestWithNamespace("eth", port, "getTransactionByBlockNumberAndIndex", blockNumber, retrievalTxIndex)
			require.NotNil(t, blockNumberAndIndexResult["result"])
			txFromBlockNumberAndIndex := blockNumberAndIndexResult["result"].(map[string]interface{})
			txIndexFromBlockNumberAndIndex := txFromBlockNumberAndIndex["transactionIndex"].(string)

			blockByHashResult := sendRequestWithNamespace("eth", port, "getBlockByHash", blockHash, true)
			require.NotNil(t, blockByHashResult["result"])
			blockByHash := blockByHashResult["result"].(map[string]interface{})
			transactionsByHash := blockByHash["transactions"].([]interface{})
			require.Equal(t, len(transactionsByHash), numberOfEVMTransactions)
			txFromBlockByHash := transactionsByHash[correctTxIndex].(map[string]interface{})
			txIndexFromBlockByHash := txFromBlockByHash["transactionIndex"].(string)

			blockByNumberResult := sendRequestWithNamespace("eth", port, "getBlockByNumber", blockNumber, true)
			require.NotNil(t, blockByNumberResult["result"])
			blockByNumber := blockByNumberResult["result"].(map[string]interface{})
			transactionsByNumber := blockByNumber["transactions"].([]interface{})
			require.Equal(t, len(transactionsByNumber), numberOfEVMTransactions)
			txFromBlockByNumber := transactionsByNumber[correctTxIndex].(map[string]interface{})
			txIndexFromBlockByNumber := txFromBlockByNumber["transactionIndex"].(string)

			blockReceiptsResult := sendRequestWithNamespace("eth", port, "getBlockReceipts", blockHash)
			require.NotNil(t, blockReceiptsResult["result"])
			blockReceipts := blockReceiptsResult["result"].([]interface{})
			require.Equal(t, len(blockReceipts), numberOfEVMTransactions)
			var txIndexFromBlockReceipts string
			for _, receipt := range blockReceipts {
				receiptMap := receipt.(map[string]interface{})
				if receiptMap["transactionHash"] == txHash.Hex() {
					txIndexFromBlockReceipts = receiptMap["transactionIndex"].(string)
					break
				}
			}
			require.NotEmpty(t, txIndexFromBlockReceipts, "Should find transaction index in block receipts")

			allIndices := []string{
				receiptTxIndex,
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
					"Transaction index should be the same and correct across all endpoints that serve it. Expected: %d, Got: %d", correctTxIndex, actualTxIndex)
			}
		},
	)
}

func TestEVMTransactionIndexResolutionOnInput(t *testing.T) {
    t.Run("RegularBehaviour", func (t *testing.T) {
        cosmosTx1 := signAndEncodeCosmosTx(bankSendMsg(mnemonic1), mnemonic1, 7, 0)
        cosmosTx2 := signAndEncodeCosmosTx(bankSendMsg(mnemonic1), mnemonic1, 7, 1)

        tx1Data := send(0)
        signedTx1 := signTxWithMnemonic(tx1Data, mnemonic1)
        tx1 := encodeEvmTx(tx1Data, signedTx1)

        cosmosTx3 := signAndEncodeCosmosTx(bankSendMsg(mnemonic1), mnemonic1, 7, 2)

        tx2Data := send(1)
        signedTx2 := signTxWithMnemonic(tx2Data, mnemonic1)
        tx2 := encodeEvmTx(tx2Data, signedTx2)

        SetupTestServer([][][]byte{{cosmosTx1, cosmosTx2, tx1, cosmosTx3, tx2}}, mnemonicInitializer(mnemonic1)).Run(
            func(port int) {
                blockNumber := "0x2"

                blockResult := sendRequestWithNamespace("eth", port, "getBlockByNumber", blockNumber, false)
                require.NotNil(t, blockResult["result"])
                blockHash := blockResult["result"].(map[string]interface{})["hash"].(string)

                result1 := sendRequestWithNamespace("eth", port, "getTransactionByBlockNumberAndIndex", blockNumber, "0x0")
                require.NotNil(t, result1["result"])
                txFromIndex0 := result1["result"].(map[string]interface{})
                require.Equal(t, signedTx1.Hash().Hex(), txFromIndex0["hash"].(string))
                require.Equal(t, "0x0", txFromIndex0["transactionIndex"].(string))
                result4 := sendRequestWithNamespace("eth", port, "getTransactionByBlockHashAndIndex", blockHash, "0x0")
                require.NotNil(t, result4["result"])
                txFromHashIndex0 := result4["result"].(map[string]interface{})
                require.Equal(t, signedTx1.Hash().Hex(), txFromHashIndex0["hash"].(string))
                require.Equal(t, "0x0", txFromHashIndex0["transactionIndex"].(string))

                result2 := sendRequestWithNamespace("eth", port, "getTransactionByBlockNumberAndIndex", blockNumber, "0x1")
                require.NotNil(t, result2["result"])
                txFromIndex1 := result2["result"].(map[string]interface{})
                require.Equal(t, signedTx2.Hash().Hex(), txFromIndex1["hash"].(string))
                require.Equal(t, "0x1", txFromIndex1["transactionIndex"].(string))
                result5 := sendRequestWithNamespace("eth", port, "getTransactionByBlockHashAndIndex", blockHash, "0x1")
                require.NotNil(t, result5["result"])
                txFromHashIndex1 := result5["result"].(map[string]interface{})
                require.Equal(t, signedTx2.Hash().Hex(), txFromHashIndex1["hash"].(string))
                require.Equal(t, "0x1", txFromHashIndex1["transactionIndex"].(string))

                result3 := sendRequestWithNamespace("eth", port, "getTransactionByBlockNumberAndIndex", blockNumber, "0x2")
                require.Nil(t, result3["result"])
                result6 := sendRequestWithNamespace("eth", port, "getTransactionByBlockHashAndIndex", blockHash, "0x2")
                require.Nil(t, result6["result"])

                result7 := sendRequestWithNamespace("eth", port, "getTransactionByBlockNumberAndIndex", blockNumber, "0x5")
                require.Nil(t, result7["result"])
                result8 := sendRequestWithNamespace("eth", port, "getTransactionByBlockHashAndIndex", blockHash, "0x5")
                require.Nil(t, result8["result"])

            },
        )
    })

    t.Run("EVMAndCosmosIndexCollision", func (t *testing.T) {
        // Create a block where an EVM transaction has a Cosmos index that could be confused with EVM index
        // Order: [cosmos_tx, evm_tx, cosmos_tx, evm_tx]
        // Cosmos indices: 0, 1, 2, 3
        // EVM indices: 0, 1
        // If we passed index 1 as Cosmos index, it would point to the first EVM tx
        // But if we pass index 1 as EVM index, it should point to the second EVM tx

        cosmosTx1 := signAndEncodeCosmosTx(bankSendMsg(mnemonic1), mnemonic1, 7, 0)

        tx1Data := send(0)
        signedTx1 := signTxWithMnemonic(tx1Data, mnemonic1)
        tx1 := encodeEvmTx(tx1Data, signedTx1)

        cosmosTx2 := signAndEncodeCosmosTx(bankSendMsg(mnemonic1), mnemonic1, 7, 2)

        tx2Data := send(1)
        signedTx2 := signTxWithMnemonic(tx2Data, mnemonic1)
        tx2 := encodeEvmTx(tx2Data, signedTx2)

        SetupTestServer([][][]byte{{cosmosTx1, tx1, cosmosTx2, tx2}}, mnemonicInitializer(mnemonic1)).Run(
            func(port int) {
                blockNumber := "0x2"

                blockResult := sendRequestWithNamespace("eth", port, "getBlockByNumber", blockNumber, false)
                require.NotNil(t, blockResult["result"])
                blockHash := blockResult["result"].(map[string]interface{})["hash"].(string)

                result1 := sendRequestWithNamespace("eth", port, "getTransactionByBlockNumberAndIndex", blockNumber, "0x0")
                require.NotNil(t, result1["result"])
                txFromIndex0 := result1["result"].(map[string]interface{})
                require.Equal(t, signedTx1.Hash().Hex(), txFromIndex0["hash"].(string))
                require.Equal(t, "0x0", txFromIndex0["transactionIndex"].(string))
                result3 := sendRequestWithNamespace("eth", port, "getTransactionByBlockHashAndIndex", blockHash, "0x0")
                require.NotNil(t, result3["result"])
                txFromHashIndex0 := result3["result"].(map[string]interface{})
                require.Equal(t, signedTx1.Hash().Hex(), txFromHashIndex0["hash"].(string))
                require.Equal(t, "0x0", txFromHashIndex0["transactionIndex"].(string))

                result2 := sendRequestWithNamespace("eth", port, "getTransactionByBlockNumberAndIndex", blockNumber, "0x1")
                require.NotNil(t, result2["result"])
                txFromIndex1 := result2["result"].(map[string]interface{})
                require.Equal(t, signedTx2.Hash().Hex(), txFromIndex1["hash"].(string))
                require.Equal(t, "0x1", txFromIndex1["transactionIndex"].(string))
                result4 := sendRequestWithNamespace("eth", port, "getTransactionByBlockHashAndIndex", blockHash, "0x1")
                require.NotNil(t, result4["result"])
                txFromHashIndex1 := result4["result"].(map[string]interface{})
                require.Equal(t, signedTx2.Hash().Hex(), txFromHashIndex1["hash"].(string))
                require.Equal(t, "0x1", txFromHashIndex1["transactionIndex"].(string))
            },
        )
    })
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
