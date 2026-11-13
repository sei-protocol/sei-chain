package precompiles_test

import (
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/precompiles"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// A custom precompile at an upstream precompile's address is shadowed by the upstream one.
func TestCustomPrecompilesDoNotClashWithUpstream(t *testing.T) {
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(big.NewInt(1))
	rules := cfg.Rules(new(big.Int).SetUint64(math.MaxInt64), true, math.MaxUint64)
	for _, upstream := range vm.ActivePrecompiles(rules) {
		_, clash := precompiles.GetCustomPrecompiles(&utils.EmptyKeepers{})[upstream]
		require.Falsef(t, clash, "custom precompile at upstream precompile address %s", upstream.Hex())
	}
}
