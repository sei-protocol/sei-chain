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
	SetupTestServer(t, [][][]byte{{tx1Bz, tx2Bz}}, erc20Initializer()).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "getTransactionReceipt", tx2.Hash().Hex())
			// tx 1 has an EIP-7708 transfer log and a Deposit log (0x0, 0x1), so the
			// log of tx 2 has log index 0x2.
			require.Equal(t, "0x2", res["result"].(map[string]interface{})["logs"].([]interface{})[0].(map[string]interface{})["logIndex"])
		},
	)
}
