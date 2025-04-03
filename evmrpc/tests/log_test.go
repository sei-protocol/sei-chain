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
			require.Equal(t, res["error"].(map[string]interface{})["message"].(string), "a maximum of 2000 blocks worth of logs may be requested at a time")
		},
	)
}
