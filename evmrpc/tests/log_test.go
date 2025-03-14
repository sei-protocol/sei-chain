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
