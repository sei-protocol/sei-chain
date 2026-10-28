package gov

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	v2gov "github.com/sei-protocol/sei-chain/precompiles/gov"
)

// TestRequiredGasMatchesV2 pins RequiredGas to V2's flat cost for every method,
// pricing each submit method as V2's submitProposal.
func TestRequiredGasMatchesV2(t *testing.T) {
	args := make([]byte, 100)
	for name, method := range ABI.Methods {
		input := append(append([]byte{}, method.ID...), args...)
		v2Name := name
		if strings.HasPrefix(name, "submit") {
			v2Name = "submitProposal"
		}
		want := pcommon.DefaultGasCost(args, v2gov.PrecompileExecutor{}.IsTransaction(v2Name))
		require.Equal(t, want, Precompile{}.RequiredGas(input), name)
	}
	require.Equal(t, pcommon.UnknownMethodCallGas, Precompile{}.RequiredGas([]byte{1, 2, 3, 4}))
}
