package tests

// Behavior tests pinning state overrides that target precompile addresses.

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

// Sei precompiles can't be moved and ignore code overrides; builtins can be moved.
func TestBehaviorStateOverridePrecompiles(t *testing.T) {
	SetupTestServer(t, nil, mnemonicInitializer(mnemonic1)).Run(func(port int) {
		from := mnemonic1Addr.Hex()
		dest := common.HexToAddress("0x00000000000000000000000000000000000d0003")
		in := hexutil.Encode(jsonExtractInput(t))
		jsonCall := map[string]any{"from": from, "to": jsonPrecompileAddr.Hex(), "input": in}

		// moving a Sei precompile is rejected
		r := callRPC(t, port, "eth_call", map[string]any{"from": from, "to": dest.Hex(), "input": in}, "latest",
			map[string]any{jsonPrecompileAddr.Hex(): map[string]any{"movePrecompileToAddress": dest.Hex()}})
		require.NotNil(t, r.Error)
		require.Equal(t, -32000, r.Error.Code)
		require.Equal(t, "account "+jsonPrecompileAddr.Hex()+" is not a precompile", r.Error.Message)

		// code override at a Sei precompile is ignored
		res := requireRPCResult(t, port, "eth_call", jsonCall, "latest",
			map[string]any{jsonPrecompileAddr.Hex(): map[string]any{"code": sstoreCode}})
		require.Equal(t, `"`+jsonExtractOutput+`"`, string(res))

		// builtin precompile moves
		res = requireRPCResult(t, port, "eth_call", map[string]any{"from": from, "to": dest.Hex(), "input": "0x68656c6c6f"}, "latest",
			map[string]any{sha256BuiltinAddr.Hex(): map[string]any{"movePrecompileToAddress": dest.Hex()}})
		require.Equal(t, `"`+sha256HelloOutput+`"`, string(res))
	})
}
