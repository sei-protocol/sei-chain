package evmonlyapp

import (
	"math"
	"math/big"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// A custom precompile at an upstream precompile's address is shadowed by the upstream one.
func TestGovRegistryDoesNotClashWithUpstreamPrecompiles(t *testing.T) {
	rules := evmonly.DefaultChainConfig().Rules(new(big.Int).SetUint64(math.MaxInt64), true, math.MaxUint64)
	custom := NewGovRegistry(gov.DefaultParams(), 1, gov.GenesisElectorate{}, GovUpgrades{}).Addresses()
	require.NotEmpty(t, custom)
	for _, upstream := range vm.ActivePrecompiles(rules) {
		require.False(t, slices.Contains(custom, upstream), "custom precompile at upstream precompile address %s", upstream.Hex())
	}
}
