package node

import (
	"math"
	"math/big"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEVMOnlyCustomPrecompiles(t *testing.T) {
	rng := utils.TestRng()
	first := atypes.GenSecretKey(rng).Public()
	second := atypes.GenSecretKey(rng).Public()
	validators := proxy.CommitteeWeights{first: 1, second: 1}
	fc := &config.AutobahnFileConfig{Validators: []config.AutobahnValidator{
		{ValidatorKey: first, EVMVoter: utils.Some(common.HexToAddress("0xa1"))},
		{ValidatorKey: second, EVMVoter: utils.Some(common.HexToAddress("0xa2"))},
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

func TestEVMOnlyCommitteeWeights(t *testing.T) {
	rng := utils.TestRng()
	first := atypes.GenSecretKey(rng).Public()
	second := atypes.GenSecretKey(rng).Public()
	fc := &config.AutobahnFileConfig{Validators: []config.AutobahnValidator{
		{ValidatorKey: first}, {ValidatorKey: second},
	}}
	weights, err := evmOnlyCommitteeWeights(fc)
	require.NoError(t, err)
	require.Equal(t, proxy.CommitteeWeights{first: 1, second: 1}, weights)
	fc.Validators = append(fc.Validators, config.AutobahnValidator{ValidatorKey: first})
	_, err = evmOnlyCommitteeWeights(fc)
	require.Error(t, err)
}

func TestEVMOnlyGenesisElectorateUsesCommitteeWeights(t *testing.T) {
	rng := utils.TestRng()
	first := atypes.GenSecretKey(rng).Public()
	second := atypes.GenSecretKey(rng).Public()
	firstVoter := common.HexToAddress("0xa1")
	secondVoter := common.HexToAddress("0xa2")
	weights := proxy.CommitteeWeights{second: 9, first: math.MaxInt64 + 1}
	fc := &config.AutobahnFileConfig{Validators: []config.AutobahnValidator{
		{ValidatorKey: first, EVMVoter: utils.Some(firstVoter)},
		{ValidatorKey: second, EVMVoter: utils.Some(secondVoter)},
	}}
	want := gov.GenesisElectorate{
		firstVoter:  new(big.Int).SetUint64(math.MaxInt64 + 1),
		secondVoter: big.NewInt(9),
	}
	electorate, err := evmOnlyGenesisElectorate(fc, weights)
	require.NoError(t, err)
	require.Equal(t, want, electorate)
	slices.Reverse(fc.Validators)
	electorate, err = evmOnlyGenesisElectorate(fc, weights)
	require.NoError(t, err)
	require.Equal(t, want, electorate)
	delete(weights, first)
	_, err = evmOnlyGenesisElectorate(fc, weights)
	require.Error(t, err)
}
