package tests

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestFeeHistoryExcludeAnteFailedTxs(t *testing.T) {
	tx1Bz := signAndEncodeTx(depositErc20(1), erc20DeployerMnemonics)
	tx2Data := sendErc20(2)
	tx2 := signTxWithMnemonic(sendErc20(1), erc20DeployerMnemonics) // should fail ante due to stale nonce
	tx2Bz := encodeEvmTx(tx2Data, tx2)
	SetupTestServer([][][]byte{{tx1Bz, tx2Bz}}, erc20Initializer()).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "feeHistory", 1, "0x2", []float64{50, 60, 70})
			reward := res["result"].(map[string]interface{})["reward"].([]interface{})[0].([]interface{})
			for _, r := range reward {
				sr := r.(string)
				n := new(big.Int)
				_, ok := n.SetString(sr, 0)
				require.True(t, ok)
				require.NotEqual(t, -1, n.Cmp(common.Big0))
			}
		},
	)
}
