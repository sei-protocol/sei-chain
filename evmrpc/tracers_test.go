package evmrpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateBlockAccess(t *testing.T) {
	params := BlockValidationParams{
		MaxBlockLookback: 100,
		LatestHeight:     200,
	}

	// Case 1: valid block in range
	err := ValidateBlockAccess(150, params)
	require.NoError(t, err, "block within lookback should pass")

	// Case 2: block too far back
	err = ValidateBlockAccess(50, params)
	require.Error(t, err, "block outside lookback should error")

	// Case 3: exactly at edge
	err = ValidateBlockAccess(100, params)
	require.NoError(t, err, "edge block should still pass")

	// Case 4: negative lookback disables pruning checks
	params.MaxBlockLookback = -1
	err = ValidateBlockAccess(1, params)
	require.NoError(t, err, "negative lookback should bypass checks")

	// Case 5: block beyond latest height
	params.MaxBlockLookback = 100
	err = ValidateBlockAccess(250, params)
	require.Error(t, err, "block newer than latest height should error")
}

func TestTraceTransactionTimeout(t *testing.T) {
	require.Eventually(t, func() bool {
		args := map[string]interface{}{"tracer": "callTracer"}
		resObj := sendRequestStrictWithNamespace(
			t,
			"debug",
			"traceTransaction",
			DebugTraceHashHex,
			args,
		)

		errObj, ok := resObj["error"].(map[string]interface{})
		if ok {
			return errObj["message"].(string) != ""
		}
		return false
	}, 10*time.Second, 500*time.Millisecond)
}

func TestTraceBlockByNumberLookbackLimit(t *testing.T) {
	// Using the strict server (look-back = 1). Block 0 is far behind.
	resObj := sendRequestStrictWithNamespace(
		t,
		"sei",
		"traceBlockByNumberExcludeTraceFail",
		"0x0",                    // genesis block
		map[string]interface{}{}, // empty TraceConfig
	)

	errObj, ok := resObj["error"].(map[string]interface{})
	require.True(t, ok, "expected look-back guard to trigger")
	require.NotEmpty(t, errObj["message"].(string))
}
