package evmonlyapp

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// stakingTestEpochLength is the epoch length of the staking test chains: epochs close at roads 3,
// 6 and 9.
const stakingTestEpochLength types.RoadHeight = 3

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

// corruptEpochSnapshot stores, at every EndBlock, an epoch 0 snapshot longer
// than staking allows.
type corruptEpochSnapshot struct {
	staking.Precompile
}

func (corruptEpochSnapshot) EndBlock(_ precompiles.BlockContext, state precompiles.State) ([]error, error) {
	// Slot 2 of StakingStorage maps each epoch to its snapshot, and slot 3 holds
	// the epoch after the latest.
	state.SetState(staking.Address, solstorage.MapUint64(solstorage.Slot(2), 0), common.BigToHash(big.NewInt(101)))
	state.SetState(staking.Address, solstorage.Slot(3), common.BigToHash(big.NewInt(1)))
	return nil, nil
}

// closingEpochRecorder is staking, with each EndBlock only recording the epoch
// its block closes.
type closingEpochRecorder struct {
	staking.Precompile
	closed *[]utils.Option[uint64]
}

func (r closingEpochRecorder) EndBlock(block precompiles.BlockContext, _ precompiles.State) ([]error, error) {
	*r.closed = append(*r.closed, block.ClosingEpoch)
	return nil, nil
}

// newStakingEVMOnlyTestApp returns an initialized application configured with
// configured, running contract as staking, with epochs of stakingTestEpochLength.
func newStakingEVMOnlyTestApp(t *testing.T, configured abevm.CommitteeWeights, contract utils.Option[precompiles.Contract]) abevm.Application {
	t.Helper()
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage := openEVMOnlyTestStorage(t, storageConfig)
	registry := utils.None[precompiles.Registry]()
	if contract, ok := contract.Get(); ok {
		registry = utils.Some[precompiles.Registry](precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{staking.Address: contract}, staking.Address))
	}
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID,
		storage,
		openEVMOnlyTestEpochsWithLength(t, storage, configured, 1, stakingTestEpochLength),
		gigaconfig.DefaultConfig.Execution,
		registry,
		utils.None[GenesisAlloc](),
		utils.None[string](),
	)
	require.NoError(t, err)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })
	require.NoError(t, app.InitChain(evmOnlyTestInitChain()))
	return app
}

// runEVMOnlyTestBlocks finalizes an empty block at each height from first through last.
func runEVMOnlyTestBlocks(t *testing.T, app abevm.Application, first, last int64) {
	t.Helper()
	for height := first; height <= last; height++ {
		finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, height))
	}
}

// committeeWeights returns the voting weight of each member of epoch's committee in app's registry.
func committeeWeights(t *testing.T, app abevm.Application, epoch types.EpochIndex) map[types.PublicKey]uint64 {
	t.Helper()
	registry, err := app.EpochRegistry()
	require.NoError(t, err)
	e, err := registry.Epoch(epoch)
	require.NoError(t, err)
	weights := map[types.PublicKey]uint64{}
	for lane := range e.Committee().Lanes().All() {
		weights[lane.Validator] = e.Committee().Weight(lane.Validator)
	}
	return weights
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

func TestEVMOnlyApplicationTellsEndBlockersWhichEpochABlockCloses(t *testing.T) {
	var closed []utils.Option[uint64]
	app := newStakingEVMOnlyTestApp(t, nil, utils.Some[precompiles.Contract](closingEpochRecorder{closed: &closed}))
	runEVMOnlyTestBlocks(t, app, 1, 7)
	none := utils.None[uint64]()
	require.Equal(t, []utils.Option[uint64]{none, none, utils.Some[uint64](0), none, none, utils.Some[uint64](1), none}, closed)
}

func TestEVMOnlyApplicationCommitsEachDerivedEpochToStakingsSnapshot(t *testing.T) {
	configuredKey := autobahnKey(ed25519.TestSecretKey([]byte("configured")).Public())
	key1 := ed25519.TestSecretKey([]byte("validator-1")).Public()
	key2 := ed25519.TestSecretKey([]byte("validator-2")).Public()
	app := newStakingEVMOnlyTestApp(t, abevm.CommitteeWeights{configuredKey: 1}, utils.Some[precompiles.Contract](stakingGenesis{validators: []staking.GenesisValidator{
		testGenesisValidator(common.HexToAddress("0xa2"), key2, 20),
		testGenesisValidator(common.HexToAddress("0xa1"), key1, 30),
	}}))
	configured := map[types.PublicKey]uint64{configuredKey: 1}

	runEVMOnlyTestBlocks(t, app, 1, 3)
	require.Equal(t, configured, committeeWeights(t, app, 0))
	require.Equal(t, configured, committeeWeights(t, app, 1))
	staked := map[types.PublicKey]uint64{autobahnKey(key1): 30, autobahnKey(key2): 20}
	require.Equal(t, staked, committeeWeights(t, app, 2), "the end of epoch 0 derives epoch 2 from its snapshot")

	runEVMOnlyTestBlocks(t, app, 4, 6)
	require.Equal(t, staked, committeeWeights(t, app, 3))
}

func TestEVMOnlyApplicationCommitsToTheConfiguredValidatorsUntilStakingSnapshotsASet(t *testing.T) {
	configuredKey := autobahnKey(ed25519.TestSecretKey([]byte("configured")).Public())
	configured := map[types.PublicKey]uint64{configuredKey: 1}
	for name, contract := range map[string]utils.Option[precompiles.Contract]{
		"without staking":           utils.None[precompiles.Contract](),
		"with nothing bonded":       utils.Some[precompiles.Contract](stakingGenesis{}),
		"before staking has params": utils.Some[precompiles.Contract](staking.Precompile{}),
	} {
		t.Run(name, func(t *testing.T) {
			app := newStakingEVMOnlyTestApp(t, abevm.CommitteeWeights{configuredKey: 1}, contract)
			runEVMOnlyTestBlocks(t, app, 1, 3)
			require.Equal(t, configured, committeeWeights(t, app, 2))
		})
	}
}

func TestEVMOnlyApplicationFailsTheBlockThatClosesAnEpochOnACorruptSnapshot(t *testing.T) {
	app := newStakingEVMOnlyTestApp(t, nil, utils.Some[precompiles.Contract](corruptEpochSnapshot{}))
	runEVMOnlyTestBlocks(t, app, 1, 2)
	_, err := app.FinalizeBlock(t.Context(), evmOnlyTestBlock(app, 3))
	require.Error(t, err)
	require.Contains(t, err.Error(), "read staking's committee at road 3")
}

// autobahnKey returns key as consensus reads it.
func autobahnKey(key ed25519.PublicKey) types.PublicKey {
	return utils.OrPanic1(types.PublicKeyFromBytes(key.Bytes()))
}
