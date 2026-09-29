package node

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func governanceFileConfig(t *testing.T) *config.AutobahnFileConfig {
	validators := []config.AutobahnValidator{
		makeValidator([]byte("gov-validator-0"), []byte("gov-node-0"), "localhost:26660"),
		makeValidator([]byte("gov-validator-1"), []byte("gov-node-1"), "localhost:26661"),
	}
	validators[0].EVMVoter = utils.Some(common.HexToAddress("0x00000000000000000000000000000000000000a1"))
	validators[1].EVMVoter = utils.Some(common.HexToAddress("0x00000000000000000000000000000000000000b2"))
	fc := defaultFileConfig(t, validators)
	fc.EVMGovernance = utils.Some(gov.DefaultParams())
	return fc
}

func TestEVMOnlyCustomPrecompilesDisabledWithoutGovernance(t *testing.T) {
	fc := defaultFileConfig(t, []config.AutobahnValidator{makeValidator([]byte("v"), []byte("n"), "localhost:26660")})
	validators, err := evmOnlyValidatorUpdates(fc)
	require.NoError(t, err)
	registry, err := evmOnlyCustomPrecompiles(fc, validators)
	require.NoError(t, err)
	require.False(t, registry.IsPresent())
}

func TestEVMOnlyCustomPrecompilesRegistersGovernance(t *testing.T) {
	fc := governanceFileConfig(t)
	require.NoError(t, fc.Validate())
	validators, err := evmOnlyValidatorUpdates(fc)
	require.NoError(t, err)
	opt, err := evmOnlyCustomPrecompiles(fc, validators)
	require.NoError(t, err)
	registry, ok := opt.Get()
	require.True(t, ok)
	require.Equal(t, []common.Address{gov.Address}, registry.Addresses())
	_, ok = registry.Get(gov.Address)
	require.True(t, ok)
}

func TestEVMOnlyCustomPrecompilesRejectsBadVoters(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(fc *config.AutobahnFileConfig, validators []abci.ValidatorUpdate)
	}{
		{"missing_voter", func(fc *config.AutobahnFileConfig, _ []abci.ValidatorUpdate) {
			fc.Validators[1].EVMVoter = utils.None[common.Address]()
		}},
		{"shared_voter", func(fc *config.AutobahnFileConfig, _ []abci.ValidatorUpdate) {
			fc.Validators[1].EVMVoter = fc.Validators[0].EVMVoter
		}},
		{"negative_power", func(_ *config.AutobahnFileConfig, validators []abci.ValidatorUpdate) {
			validators[0].Power = -1
		}},
		{"zero_power", func(_ *config.AutobahnFileConfig, validators []abci.ValidatorUpdate) {
			validators[0].Power = 0
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := governanceFileConfig(t)
			validators, err := evmOnlyValidatorUpdates(fc)
			require.NoError(t, err)
			tc.mutate(fc, validators)
			_, err = evmOnlyCustomPrecompiles(fc, validators)
			require.Error(t, err)
		})
	}
}

func TestPrepareApplicationAutobahnRejectsInvalidGovernance(t *testing.T) {
	fc := governanceFileConfig(t)
	fc.Validators[1].EVMVoter = utils.None[common.Address]()
	_, _, err := prepareApplication(t.Context(), &config.Config{
		BaseConfig:         config.BaseConfig{FastCheckTx: true},
		AutobahnConfigFile: writeAutobahnConfig(t, fc),
	}, abci.BaseApplication{}, gigaconfig.DefaultConfig)
	require.Error(t, err)
}

func TestPrepareApplicationAutobahnWithGovernance(t *testing.T) {
	fc := governanceFileConfig(t)
	prepared, storage, err := prepareApplication(t.Context(), &config.Config{
		BaseConfig:         config.BaseConfig{FastCheckTx: true},
		AutobahnConfigFile: writeAutobahnConfig(t, fc),
	}, abci.BaseApplication{}, gigaconfig.DefaultConfig)
	require.NoError(t, err)
	manager, ok := storage.Get()
	require.True(t, ok)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	require.Equal(t, "evmonly", prepared.Info().Data)
}
