package upgrade_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
)

// TestRequiredGasMatchesV2 pins RequiredGas to V2's flat query cost.
func TestRequiredGasMatchesV2(t *testing.T) {
	args := make([]byte, 100)
	for name, method := range upgrade.ABI.Methods {
		input := append(append([]byte{}, method.ID...), args...)
		require.Equal(t, pcommon.DefaultGasCost(args, false), upgrade.Precompile{}.RequiredGas(input), name)
	}
	require.Equal(t, pcommon.UnknownMethodCallGas, upgrade.Precompile{}.RequiredGas([]byte{1, 2, 3, 4}))
}
