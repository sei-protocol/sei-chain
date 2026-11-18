package node

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestEVMOnlyCustomPrecompiles(t *testing.T) {
	validators := []proxy.ValidatorUpdate{{Power: 1}, {Power: 1}}
	fc := &config.AutobahnFileConfig{Validators: []config.AutobahnValidator{
		{EVMVoter: utils.Some(common.HexToAddress("0xa1"))},
		{EVMVoter: utils.Some(common.HexToAddress("0xa2"))},
	}}

	none, err := evmOnlyCustomPrecompiles(&config.AutobahnFileConfig{Validators: make([]config.AutobahnValidator, 2)}, validators, nil)
	require.NoError(t, err)
	require.False(t, none.IsPresent(), "no evm_governance, no precompiles")

	fc.EVMGovernance = utils.Some(config.EVMGovernance{Params: config.NewEVMGovernanceParams(gov.DefaultParams())})
	registry, err := evmOnlyCustomPrecompiles(fc, validators, nil)
	require.NoError(t, err)
	r, ok := registry.Get()
	require.True(t, ok)
	_, ok = r.Get(gov.Address)
	require.True(t, ok)
	require.Equal(t, []common.Address{gov.Address}, r.EndBlockers())
	require.Equal(t, []common.Address{upgrade.Address}, r.BeginBlockers())

	fc.Validators[1].EVMVoter = utils.None[common.Address]()
	_, err = evmOnlyCustomPrecompiles(fc, validators, nil)
	require.Error(t, err)
}
