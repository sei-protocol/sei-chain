package tests

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBehaviorZeroBlockNumberIsEarliest pins that 0x0 resolves like "earliest" on state, tx and fee endpoints.
func TestBehaviorZeroBlockNumberIsEarliest(t *testing.T) {
	txBz := signAndEncodeTx(send(0), mnemonic1)
	SetupTestServer(t, [][][]byte{{txBz}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			addr := getAddrWithMnemonic(mnemonic1).Hex()
			txByIndex := map[string]any{}
			for _, tag := range []string{"0x0", "earliest"} {
				res := sendRequestWithNamespace("eth", port, "getBalance", addr, tag)
				require.Nil(t, res["error"], tag)
				require.Equal(t, "0x21e19e0c9bab2400000", res["result"], tag)

				res = sendRequestWithNamespace("eth", port, "getTransactionCount", addr, tag)
				require.Nil(t, res["error"], tag)
				require.Equal(t, "0x0", res["result"], tag)

				res = sendRequestWithNamespace("eth", port, "getTransactionByBlockNumberAndIndex", tag, "0x0")
				require.Nil(t, res["error"], tag)
				txByIndex[tag] = res["result"]

				res = sendRequestWithNamespace("eth", port, "feeHistory", "0x1", tag, []any{})
				require.Nil(t, res["error"], tag)
				require.Equal(t, "0x1", res["result"].(map[string]any)["oldestBlock"], tag)
			}
			require.Equal(t, txByIndex["earliest"], txByIndex["0x0"])
		},
	)
}
