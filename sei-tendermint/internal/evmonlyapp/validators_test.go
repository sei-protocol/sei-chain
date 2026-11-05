package evmonlyapp

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// stakingGenesis is staking with the params and validators it writes at its
// first EndBlock.
type stakingGenesis struct {
	staking.Precompile
	validators []staking.GenesisValidator
}

func (g stakingGenesis) EndBlock(block precompiles.BlockContext, state precompiles.State) ([]error, error) {
	if !staking.HasParams(state) {
		if err := staking.SetParams(state, staking.DefaultParams()); err != nil {
			return nil, err
		}
		for _, v := range g.validators {
			if err := g.AddGenesisValidator(block, state, v); err != nil {
				return nil, err
			}
		}
	}
	return g.Precompile.EndBlock(block, state)
}

// corruptBondedSet stores a bonded set longer than staking allows at every
// EndBlock.
type corruptBondedSet struct {
	staking.Precompile
}

func (corruptBondedSet) EndBlock(_ precompiles.BlockContext, state precompiles.State) ([]error, error) {
	// Slot 0 of StakingStorage holds the bonded set's length.
	state.SetState(staking.Address, common.Hash{}, common.BigToHash(big.NewInt(101)))
	return nil, nil
}

// newStakingEVMOnlyTestApp returns an initialized application serving the
// configured validators and running staking as contract.
func newStakingEVMOnlyTestApp(t *testing.T, configured []abci.ValidatorUpdate, contract precompiles.Contract) abci.Application {
	t.Helper()
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage := openEVMOnlyTestStorage(t, storageConfig)
	registry := precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{staking.Address: contract}, staking.Address)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID,
		configured,
		storage,
		evmonly.NewFlatKVChangeSetEncoder(),
		gigaconfig.DefaultConfig.Execution,
		utils.Some[precompiles.Registry](registry),
	)
	require.NoError(t, err)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	return app
}

// testSei returns n SEI in wei.
func testSei(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000_000_000_000_000))
}

func testGenesisValidator(operator common.Address, key ed25519.PublicKey, self int64) staking.GenesisValidator {
	return staking.GenesisValidator{
		Operator:          operator,
		ConsensusPubkey:   common.BytesToHash(key.Bytes()),
		SelfDelegation:    testSei(self),
		MinSelfDelegation: testSei(1),
	}
}

func TestEVMOnlyApplicationServesStakingsBondedSet(t *testing.T) {
	configured := []abci.ValidatorUpdate{{PubKey: crypto.PubKeyToProto(ed25519.TestSecretKey([]byte("configured")).Public()), Power: 1}}
	key1 := ed25519.TestSecretKey([]byte("validator-1")).Public()
	key2 := ed25519.TestSecretKey([]byte("validator-2")).Public()
	app := newStakingEVMOnlyTestApp(t, configured, stakingGenesis{validators: []staking.GenesisValidator{
		testGenesisValidator(common.HexToAddress("0xa2"), key2, 20),
		testGenesisValidator(common.HexToAddress("0xa1"), key1, 30),
	}})
	require.Equal(t, configured, app.GetValidators(), "the configured validators serve until staking bonds a set")

	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1))
	require.Equal(t, []abci.ValidatorUpdate{
		{PubKey: crypto.PubKeyToProto(key1), Power: 30},
		{PubKey: crypto.PubKeyToProto(key2), Power: 20},
	}, app.GetValidators())
}

func TestEVMOnlyApplicationServesNoValidatorsFromACorruptBondedSet(t *testing.T) {
	app := newStakingEVMOnlyTestApp(t, []abci.ValidatorUpdate{{Power: 1}}, corruptBondedSet{})
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1))
	require.Empty(t, app.GetValidators())
}
