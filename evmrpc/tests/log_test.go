package tests

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetLogs(t *testing.T) {
	tx1Bz := signAndEncodeTx(depositErc20(1), erc20DeployerMnemonics)
	tx2Bz := signAndEncodeTx(sendErc20(2), erc20DeployerMnemonics)
	SetupTestServer([][][]byte{{tx1Bz, tx2Bz}}, erc20Initializer()).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "getLogs", map[string]interface{}{
				"toBlock": "latest",
				"address": erc20Addr.Hex(),
			})
			require.Len(t, res["result"], 2)
		},
	)
}

func TestGetLogsRangeTooWide(t *testing.T) {
	SetupTestServer([][][]byte{{}}, erc20Initializer()).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "getLogs", map[string]interface{}{
				"fromBlock": "0x1",
				"toBlock":   "0x7D2",
				"address":   erc20Addr.Hex(),
			})
			require.Equal(t, res["error"].(map[string]interface{})["message"].(string), "block range too large (2002), maximum allowed is 2000 blocks")
		},
	)
}

func TestGetLogIndex(t *testing.T) {
	cw20 := "sei18cszlvm6pze0x9sz32qnjq4vtd45xehqs8dq7cwy8yhq35wfnn3quh5sau" // hardcoded
	tx0 := signAndEncodeCosmosTx(transferCW20Msg(mnemonic1, cw20), mnemonic1, 7, 0)
	tx1Bz := signAndEncodeTx(depositErc20(1), erc20DeployerMnemonics)
	tx2Bz := signAndEncodeTx(sendErc20(2), erc20DeployerMnemonics)
	SetupTestServer([][][]byte{{tx0, tx1Bz, tx2Bz}}, mnemonicInitializer(mnemonic1), cw20Initializer(mnemonic1), erc20Initializer()).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "getLogs", map[string]interface{}{
				"toBlock": "latest",
				"address": erc20Addr.Hex(),
			})
			require.Len(t, res["result"], 2)
			require.Equal(t, "0x0", res["result"].([]interface{})[0].(map[string]interface{})["transactionIndex"])
			require.Equal(t, "0x1", res["result"].([]interface{})[1].(map[string]interface{})["transactionIndex"])
		},
	)
}
