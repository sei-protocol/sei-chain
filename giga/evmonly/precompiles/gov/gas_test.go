package gov_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	v2gov "github.com/sei-protocol/sei-chain/precompiles/gov"
)

// TestRequiredGasMatchesV2 pins RequiredGas to V2's flat cost for every method.
func TestRequiredGasMatchesV2(t *testing.T) {
	args := make([]byte, 100)
	for name, method := range gov.ABI.Methods {
		input := append(append([]byte{}, method.ID...), args...)
		want := pcommon.DefaultGasCost(args, v2gov.PrecompileExecutor{}.IsTransaction(name))
		require.Equal(t, want, gov.Precompile{}.RequiredGas(input), name)
	}
	require.Equal(t, pcommon.UnknownMethodCallGas, gov.Precompile{}.RequiredGas([]byte{1, 2, 3, 4}))
}
