package wasmd

import (
	"math"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

func TestParseCoinsMetersLogicalPayloads(t *testing.T) {
	shared := []byte("[]")
	payloads := [][]byte{shared, shared, shared}
	wantGas := coinJSONParseFixedGas*uint64(len(payloads)) +
		coinJSONParseGasPerByte*uint64(len(shared)*len(payloads))

	ctx := sdk.Context{}.WithGasMeter(sdk.NewGasMeter(math.MaxUint64, 1, 1))
	coins, err := parseCoins(ctx, payloads...)
	require.NoError(t, err)
	require.Len(t, coins, len(payloads))
	require.Equal(t, wantGas, ctx.GasMeter().GasConsumed())
}

func TestParseCoinsChargesBeforeUnmarshal(t *testing.T) {
	payloads := [][]byte{[]byte("{"), []byte("[]")}
	parseGas := coinJSONParseGas(payloads)
	ctx := sdk.Context{}.WithGasMeter(sdk.NewGasMeter(parseGas-1, 1, 1))

	require.PanicsWithValue(t, sdk.ErrorOutOfGas{Descriptor: "wasmd coin JSON parse"}, func() {
		_, _ = parseCoins(ctx, payloads...)
	})
}

func TestCoinJSONParseGasSaturates(t *testing.T) {
	require.Equal(t, uint64(math.MaxUint64), coinJSONParseGasFor(math.MaxUint64, math.MaxUint64))
}

func TestValidateExecuteBatchSize(t *testing.T) {
	require.NoError(t, validateExecuteBatchSize(MaxExecuteBatchMessages))
	err := validateExecuteBatchSize(MaxExecuteBatchMessages + 1)
	require.EqualError(t, err, "too many execute_batch messages: got 101, maximum is 100")
}
