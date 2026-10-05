package tests

// Behavior tests pinning eth_estimateGasAfterCalls, eth_createAccessList and hex quantity decoding.

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

// PUSH1 1 PUSH1 0 SSTORE STOP
const sstoreCode = "0x600160005500"

// eth_estimateGasAfterCalls with state overrides and 0, 1 prior calls.
func TestBehaviorEstimateGasAfterCallsOverrides(t *testing.T) {
	SetupTestServer(t, nil, mnemonicInitializer(mnemonic1)).Run(func(port int) {
		tgt := common.HexToAddress("0x00000000000000000000000000000000000d0001")
		other := common.HexToAddress("0x00000000000000000000000000000000000d0002")
		args := map[string]any{"from": mnemonic1Addr.Hex(), "to": tgt.Hex()}
		overrides := map[string]any{tgt.Hex(): map[string]any{"code": sstoreCode}}
		unrelatedCall := map[string]any{"from": mnemonic1Addr.Hex(), "to": other.Hex()}

		// eth_estimateGas baselines with and without override
		require.Equal(t, `"0xa9da"`, string(requireRPCResult(t, port, "eth_estimateGas", args, "latest", overrides)))
		require.Equal(t, `"0x5208"`, string(requireRPCResult(t, port, "eth_estimateGas", args, "latest")))

		// zero prior calls: overrides dropped
		// NOTE: known divergence: go-ethereum v1.17.7 port applies overrides with zero calls
		require.Equal(t, `"0x5208"`, string(requireRPCResult(t, port, "eth_estimateGasAfterCalls", args, []any{}, "latest", overrides)))
		require.Equal(t, `"0x5208"`, string(requireRPCResult(t, port, "eth_estimateGasAfterCalls", args, nil, "latest", overrides)))

		// one prior call: overrides applied
		require.Equal(t, `"0xa9da"`, string(requireRPCResult(t, port, "eth_estimateGasAfterCalls", args, []any{unrelatedCall}, "latest", overrides)))
		// prior call already wrote the slot
		require.Equal(t, `"0x5b93"`, string(requireRPCResult(t, port, "eth_estimateGasAfterCalls", args, []any{args}, "latest", overrides)))
		// one call, no overrides
		require.Equal(t, `"0x5208"`, string(requireRPCResult(t, port, "eth_estimateGasAfterCalls", args, []any{unrelatedCall}, "latest")))
	})
}

// eth_createAccessList excludes precompiles reached directly or via proxies.
func TestBehaviorCreateAccessListPrecompile(t *testing.T) {
	SetupTestServer(t, nil, mnemonicInitializer(mnemonic1), behaviorProxyInitializer()).Run(func(port int) {
		in := hexutil.Encode(jsonExtractInput(t))
		from := mnemonic1Addr.Hex()

		// direct Sei precompile
		res := requireRPCResult(t, port, "eth_createAccessList",
			map[string]any{"from": from, "to": jsonPrecompileAddr.Hex(), "input": in, "gas": "0x30d40"}, "latest")
		require.JSONEq(t, `{"accessList":[],"gasUsed":"0x5c04"}`, string(res))

		// proxy -> Sei precompile
		res = requireRPCResult(t, port, "eth_createAccessList",
			map[string]any{"from": from, "to": jsonProxyAddr.Hex(), "input": in, "gas": "0x30d40"}, "latest")
		require.JSONEq(t, `{"accessList":[],"gasUsed":"0x6683"}`, string(res))

		// proxy -> sha256
		res = requireRPCResult(t, port, "eth_createAccessList",
			map[string]any{"from": from, "to": sha256ProxyAddr.Hex(), "input": "0x68656c6c6f", "gas": "0x30d40"}, "latest")
		require.JSONEq(t, `{"accessList":[],"gasUsed":"0x533d"}`, string(res))

		// outer proxy -> inner proxy -> Sei precompile: only the inner proxy is listed
		res = requireRPCResult(t, port, "eth_createAccessList",
			map[string]any{"from": from, "to": outerProxyAddr.Hex(), "input": in, "gas": "0x30d40"}, "latest")
		require.JSONEq(t, `{"accessList":[{"address":"0x00000000000000000000000000000000000b0001","storageKeys":[]}],"gasUsed":"0x709e"}`, string(res))
	})
}

// "0x01" is rejected by hexutil.Big args and accepted by U256 authorizationList fields.
func TestBehaviorHexQuantityLeadingZeros(t *testing.T) {
	SetupTestServer(t, nil, mnemonicInitializer(mnemonic1)).Run(func(port int) {
		tgt := common.HexToAddress("0x00000000000000000000000000000000000d0001")
		from := mnemonic1Addr.Hex()

		r := callRPC(t, port, "eth_call", map[string]any{"from": from, "to": tgt.Hex(), "value": "0x01"}, "latest")
		require.NotNil(t, r.Error)
		require.Equal(t, -32602, r.Error.Code)
		require.Equal(t, "invalid argument 0: json: cannot unmarshal hex number with leading zero digits into Go value of type *hexutil.Big", r.Error.Message)

		r = callRPC(t, port, "eth_call", map[string]any{"from": from, "to": tgt.Hex()}, "latest",
			map[string]any{tgt.Hex(): map[string]any{"balance": "0x01"}})
		require.NotNil(t, r.Error)
		require.Equal(t, -32602, r.Error.Code)
		require.Equal(t, "invalid argument 2: json: cannot unmarshal hex number with leading zero digits into Go value of type *hexutil.Big", r.Error.Message)

		// leading-zero and canonical auth fields behave the same
		authLeading := []any{map[string]any{"chainId": "0x01", "address": tgt.Hex(), "nonce": "0x0", "yParity": "0x0", "r": "0x01", "s": "0x01"}}
		authCanon := []any{map[string]any{"chainId": "0x1", "address": tgt.Hex(), "nonce": "0x0", "yParity": "0x0", "r": "0x1", "s": "0x1"}}
		for _, auth := range [][]any{authLeading, authCanon} {
			args := map[string]any{"from": from, "to": tgt.Hex(), "authorizationList": auth}
			require.Equal(t, `"0x"`, string(requireRPCResult(t, port, "eth_call", args, "latest")))
			require.Equal(t, `"0xb52e"`, string(requireRPCResult(t, port, "eth_estimateGas", args, "latest")))
		}
	})
}
