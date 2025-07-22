package tests

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReceiptLogIndex(t *testing.T) {
	tx1Bz := signAndEncodeTx(depositErc20(1), erc20DeployerMnemonics)
	tx2Data := sendErc20(2)
	tx2 := signTxWithMnemonic(sendErc20(2), erc20DeployerMnemonics)
	tx2Bz := encodeEvmTx(tx2Data, tx2)
	SetupTestServer([][][]byte{{tx1Bz, tx2Bz}}, erc20Initializer()).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "getTransactionReceipt", tx2.Hash().Hex())
			// both tx 1 and tx2 have 1 log. Log of tx 1 will have log index 0x0 and
			// log of tx 2 will have log index 0x1.
			require.Equal(t, "0x1", res["result"].(map[string]interface{})["logs"].([]interface{})[0].(map[string]interface{})["logIndex"])
		},
	)
}
