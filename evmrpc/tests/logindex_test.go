package tests

import (
	"crypto/sha256"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

// Test the scenario where a transaction contains both synthetic and non-synthetic logs.
func TestGetTransactionReceiptWithMixedLogs(t *testing.T) {
	cw20 := "sei18cszlvm6pze0x9sz32qnjq4vtd45xehqs8dq7cwy8yhq35wfnn3quh5sau" // hardcoded
	testerSeiAddress := sdk.AccAddress(mixedLogTesterAddr.Bytes())
	tx0 := signAndEncodeCosmosTx(transferCW20MsgTo(mnemonic1, cw20, testerSeiAddress), mnemonic1, 9, 0)
	txData := mixedLogTesterTransfer(0, getAddrWithMnemonic(mnemonic1))
	signedTx := signTxWithMnemonic(txData, mnemonic1)
	txBz := encodeEvmTx(txData, signedTx)
	SetupTestServer(t, [][][]byte{{tx0}, {txBz}}, mixedLogTesterInitializer(), mnemonicInitializer(mnemonic1), cw20Initializer(mnemonic1, false)).Run(
		func(port int) {
			cwTxHash := common.Hash(sha256.Sum256(tx0))

			// the CW transaction in the first block should not show up in eth_getTransactionReceipt
			res := sendRequestWithNamespace("eth", port, "getTransactionReceipt", cwTxHash.Hex())
			require.Nil(t, res["result"])

			// the CW transaction in the first block should show up in sei_getTransactionReceipt,
			// with one synthetic log
			res = sendRequestWithNamespace("sei", port, "getTransactionReceipt", cwTxHash.Hex())
			logs := res["result"].(map[string]any)["logs"].([]interface{})
			require.Len(t, logs, 1)

			// the EVM transaction in the second block should show up in eth_getTransactionReceipt,
			// with two logs (one synthetic and one non-synthetic)
			res = sendRequestWithNamespace("eth", port, "getTransactionReceipt", signedTx.Hash().Hex())
			logs = res["result"].(map[string]any)["logs"].([]interface{})
			require.Len(t, logs, 2)

			// the EVM transaction in the second block should show up in sei_getTransactionReceipt,
			// with two logs (one synthetic and one non-synthetic)
			res = sendRequestWithNamespace("sei", port, "getTransactionReceipt", signedTx.Hash().Hex())
			logs = res["result"].(map[string]any)["logs"].([]interface{})
			require.Len(t, logs, 2)

			// the first block should have no receipts for eth_getBlockReceipts
			res = sendRequestWithNamespace("eth", port, "getBlockReceipts", "0x2")
			receipts := res["result"].([]interface{})
			require.Len(t, receipts, 0)

			// the first block should have one receipt for sei_getBlockReceipts,
			// with one synthetic log
			res = sendRequestWithNamespace("sei", port, "getBlockReceipts", "0x2")
			receipts = res["result"].([]interface{})
			require.Len(t, receipts, 1)
			logs = receipts[0].(map[string]any)["logs"].([]interface{})
			require.Len(t, logs, 1)

			// the second block should have one receipt for eth_getBlockReceipts,
			// with two logs (one synthetic and one non-synthetic)
			res = sendRequestWithNamespace("eth", port, "getBlockReceipts", "0x3")
			receipts = res["result"].([]interface{})
			require.Len(t, receipts, 1)
			logs = receipts[0].(map[string]any)["logs"].([]interface{})
			require.Len(t, logs, 2)

			// the second block should have one receipt for sei_getBlockReceipts,
			// with two logs (one synthetic and one non-synthetic)
			res = sendRequestWithNamespace("sei", port, "getBlockReceipts", "0x3")
			receipts = res["result"].([]interface{})
			require.Len(t, receipts, 1)
			logs = receipts[0].(map[string]any)["logs"].([]interface{})
			require.Len(t, logs, 2)

			// eth_getLogs should only return logs for the EVM transaction, which
			// has two (synthetic and non-synthetic) logs.
			res = sendRequestWithNamespace("eth", port, "getLogs", map[string]any{
				"fromBlock": "0x1",
				"toBlock":   "latest",
			})
			logs = res["result"].([]interface{})
			require.Len(t, logs, 2)
			require.Equal(t, mixedLogTesterAddr, common.HexToAddress(logs[0].(map[string]any)["address"].(string)))
			require.Equal(t, "0x0", logs[0].(map[string]any)["logIndex"])
			require.Equal(t, "0x0", logs[0].(map[string]any)["transactionIndex"])
			require.Equal(t, mixedLogTesterAddr, common.HexToAddress(logs[1].(map[string]any)["address"].(string)))
			require.Equal(t, "0x1", logs[1].(map[string]any)["logIndex"])
			require.Equal(t, "0x0", logs[1].(map[string]any)["transactionIndex"])

			// sei_getLogs should return logs for both CW and EVM transaction. The CW
			// tx has one and the EVM tx has two.
			res = sendRequestWithNamespace("sei", port, "getLogs", map[string]any{
				"fromBlock": "0x1",
				"toBlock":   "latest",
			})
			logs = res["result"].([]interface{})
			require.Len(t, logs, 3)
			require.Equal(t, mixedLogTesterAddr, common.HexToAddress(logs[0].(map[string]any)["address"].(string)))
			require.Equal(t, "0x0", logs[0].(map[string]any)["logIndex"])
			require.Equal(t, "0x0", logs[0].(map[string]any)["transactionIndex"])
			require.Equal(t, mixedLogTesterAddr, common.HexToAddress(logs[1].(map[string]any)["address"].(string)))
			require.Equal(t, "0x0", logs[1].(map[string]any)["logIndex"])
			require.Equal(t, "0x0", logs[1].(map[string]any)["transactionIndex"])
			require.Equal(t, mixedLogTesterAddr, common.HexToAddress(logs[2].(map[string]any)["address"].(string)))
			require.Equal(t, "0x1", logs[2].(map[string]any)["logIndex"])
			require.Equal(t, "0x0", logs[2].(map[string]any)["transactionIndex"])
		},
	)
}
