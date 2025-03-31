package tests

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTraceBlockByNumber(t *testing.T) {
	txBz := signAndEncodeTx(send(0), mnemonic1)
	SetupTestServer([][][]byte{{txBz}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("debug", port, "traceBlockByNumber", "0x2", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			blockHash := res["result"].([]interface{})[0].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["blockHash"]
			// assert that the block hash has been overwritten instead of the RLP hash.
			require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", blockHash.(string))
		},
	)
}
