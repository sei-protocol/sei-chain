package params_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
)

// TestRequiredGasMatchesV2 pins RequiredGas to V2's flat query cost.
func TestRequiredGasMatchesV2(t *testing.T) {
	args := make([]byte, 100)
	for name, method := range params.ABI.Methods {
		input := append(append([]byte{}, method.ID...), args...)
		require.Equal(t, pcommon.DefaultGasCost(args, false), params.Precompile{}.RequiredGas(input), name)
	}
	require.Equal(t, pcommon.UnknownMethodCallGas, params.Precompile{}.RequiredGas([]byte{1, 2, 3, 4}))
}
