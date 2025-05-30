package tests

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetBalance(t *testing.T) {
	txBz := signAndEncodeTx(send(0), mnemonic1)
	SetupTestServer([][][]byte{{txBz}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("eth", port, "getBalance", getAddrWithMnemonic(mnemonic1).Hex(), "0x1")
			balance := res["result"].(string)
			require.Equal(t, "0x21e19e0c9bab2400000", balance)
			res = sendRequestWithNamespace("eth", port, "getBalance", getAddrWithMnemonic(mnemonic1).Hex(), "0x2")
			balance = res["result"].(string)
			require.Equal(t, "0x21e19e0b6a140b5a830", balance)
			res = sendRequestWithNamespace("eth", port, "getBalance", getAddrWithMnemonic(mnemonic1).Hex(), "0x3")
			fmt.Println(res)
			err := res["error"].(map[string]interface{})
			require.Equal(t, "cannot query future blocks", err["message"].(string))
		},
	)
}
