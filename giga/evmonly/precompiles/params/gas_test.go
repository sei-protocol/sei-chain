package params_test

import (
	"os"
	"regexp"
	"strings"
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

// TestV2TypesMatchParamsSol pins V2Type's numbering to Params.sol's enum.
func TestV2TypesMatchParamsSol(t *testing.T) {
	bz, err := os.ReadFile("Params.sol")
	require.NoError(t, err)
	body := regexp.MustCompile(`(?s)enum V2Type \{(.*?)\}`).FindSubmatch(bz)
	require.NotNil(t, body)
	var names []string
	for _, n := range strings.Split(string(body[1]), ",") {
		names = append(names, strings.TrimSpace(n))
	}
	require.Equal(t, []string{"Dec", "Duration", "Coins", "DecCoins", "Int", "Uint", "Uint64", "Uint32", "Int64", "Int32", "Bool", "String", "Address", "StringList"}, names)
	require.Equal(t, params.V2Type(len(names)-1), params.V2StringList)
	require.Equal(t, params.V2Type(2), params.V2Coins)
}
