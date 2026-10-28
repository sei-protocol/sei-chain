package params_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
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
	want := []params.V2Type{
		params.V2Dec, params.V2Duration, params.V2Coins, params.V2DecCoins, params.V2Int, params.V2Uint, params.V2Uint64,
		params.V2Uint32, params.V2Int64, params.V2Int32, params.V2Bool, params.V2String, params.V2Address, params.V2StringList,
	}
	require.Equal(t, []string{"Dec", "Duration", "Coins", "DecCoins", "Int", "Uint", "Uint64", "Uint32", "Int64", "Int32", "Bool", "String", "Address", "StringList"}, names)
	for i, v := range want {
		require.Equal(t, params.V2Type(i), v, names[i])
	}

	method := params.ABI.Methods[params.ParamKeysMethod]
	out, err := params.Precompile{}.Run(&precompiles.Context{Address: params.Address}, method.ID)
	require.NoError(t, err)
	values, err := method.Outputs.Unpack(out)
	require.NoError(t, err)
	var keys []params.ParamKey
	abi.ConvertType(values[0], &keys)
	for _, k := range keys {
		require.Less(t, int(k.V2Type), len(names), "Solidity's decoder rejects a V2Type past the enum: %s", k.Key)
	}
}
