package node

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// testEpochChain is a chain whose epoch registry holds epochs 0 through 3.
type testEpochChain struct {
	genesis  EpochGenesis
	registry *epoch.Registry
}

func newTestEpochChain(t *testing.T, rng utils.Rng) testEpochChain {
	t.Helper()
	keys := utils.GenSliceN(rng, 3, atypes.GenSecretKey)
	validators := make([]atypes.PublicKey, 0, len(keys))
	for _, k := range keys {
		validators = append(validators, k.Public())
	}
	genesis := EpochGenesis{GenDoc: &types.GenesisDoc{InitialHeight: 7}, Validators: validators}
	registry, err := p2p.OpenEpochRegistry(genesis.GenDoc, validators, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, registry.TestPushEpoch(0, map[atypes.PublicKey]uint64{keys[0].Public(): 3, keys[1].Public(): 4}))
	require.NoError(t, registry.TestPushEpoch(1, map[atypes.PublicKey]uint64{keys[2].Public(): 5}))
	return testEpochChain{genesis: genesis, registry: registry}
}

// firstHeightOf returns the first road height of epoch idx.
func (c testEpochChain) firstHeightOf(idx atypes.EpochIndex) int64 {
	return int64(c.registry.MustEpoch(idx).RoadRange().First) //nolint:gosec // test roads fit in an int64.
}

// committedCheckpoint returns a FlatKV checkpoint dir whose state holds row as its epoch_registry
// row, or no row when row is None.
func committedCheckpoint(t *testing.T, row utils.Option[[]byte]) string {
	t.Helper()
	cfg := flatkvconfig.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), "flatkv")
	cfg.SnapshotInterval = 1
	wal, err := flatkv.OpenStateWAL(cfg)
	require.NoError(t, err)
	store, err := flatkv.NewCommitStore(t.Context(), cfg, wal)
	require.NoError(t, err)
	require.NoError(t, store.LoadLatest())
	changeSets := []*proto.NamedChangeSet{{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte{1}, Value: []byte{1}}}}}}
	if raw, ok := row.Get(); ok {
		changeSets = append(changeSets, &proto.NamedChangeSet{
			Name:      "autobahn",
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("epoch_registry"), Value: raw}}},
		})
	}
	require.NoError(t, store.ApplyChangeSets(1, changeSets))
	_, err = store.Commit(1)
	require.NoError(t, err)
	require.NoError(t, store.FlushSnapshots())
	dir := filepath.Join(t.TempDir(), "checkpoint")
	require.NoError(t, flatkv.HardLinkSnapshot(cfg.DataDir, 1, dir))
	require.NoError(t, store.Close())
	return dir
}

// seedLayout returns a layout whose epoch registry is seeded from checkpoint at height.
func seedLayout(t *testing.T, genesis EpochGenesis, checkpoint string, height int64) (AutobahnLayout, error) {
	t.Helper()
	psd := t.TempDir()
	layout := AutobahnLayout{PersistentStateDir: psd, EpochRegistryDir: epoch.SnapshotDir(psd)}
	staging := filepath.Join(t.TempDir(), "staging")
	if err := PrepareEpochRegistry(genesis, checkpoint, height, staging); err != nil {
		return layout, err
	}
	return layout, InstallEpochRegistry(layout, staging)
}

func openLayoutRegistry(t *testing.T, genesis EpochGenesis, layout AutobahnLayout) *epoch.Registry {
	t.Helper()
	registry, err := p2p.OpenEpochRegistry(genesis.GenDoc, genesis.Validators, layout.PersistentStateDir)
	require.NoError(t, err)
	return registry
}

func TestSeedEpochRegistryFromEpochRegistryRow(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)
	checkpoint := committedCheckpoint(t, utils.Some(chain.registry.MarshalSnapshot()))

	layout, err := seedLayout(t, chain.genesis, checkpoint, chain.firstHeightOf(2))
	require.NoError(t, err)

	registry := openLayoutRegistry(t, chain.genesis, layout)
	for idx := atypes.EpochIndex(0); idx <= 3; idx++ {
		require.True(t, epoch.EpochEqual(chain.registry.MustEpoch(idx), registry.MustEpoch(idx)))
	}
}

func TestInstallEpochRegistryWithoutRowReplacesTheOldSnapshotWithGenesis(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)
	psd := t.TempDir()
	layout := AutobahnLayout{PersistentStateDir: psd, EpochRegistryDir: epoch.SnapshotDir(psd)}
	require.NoError(t, epoch.RestoreSnapshot(psd, utils.Some(chain.registry.MarshalSnapshot())))
	staging := filepath.Join(t.TempDir(), "staging")

	require.NoError(t, PrepareEpochRegistry(chain.genesis, committedCheckpoint(t, utils.None[[]byte]()), chain.firstHeightOf(0), staging))
	require.True(t, epoch.EpochEqual(chain.registry.MustEpoch(3), openLayoutRegistry(t, chain.genesis, layout).MustEpoch(3)))
	require.NoError(t, InstallEpochRegistry(layout, staging))

	registry := openLayoutRegistry(t, chain.genesis, layout)
	require.True(t, epoch.EpochEqual(chain.registry.MustEpoch(1), registry.MustEpoch(1)))
	_, err := registry.Epoch(2)
	require.ErrorIs(t, err, atypes.ErrNotFound)
}

func TestPrepareEpochRegistryRefusesAHeightWithoutTheNextEpoch(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)
	for name, tc := range map[string]struct {
		row    utils.Option[[]byte]
		height int64
	}{
		"row through epoch 3, height in epoch 3": {utils.Some(chain.registry.MarshalSnapshot()), chain.firstHeightOf(3)},
		"no row, height in epoch 1":              {utils.None[[]byte](), chain.firstHeightOf(1)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := seedLayout(t, chain.genesis, committedCheckpoint(t, tc.row), tc.height)
			require.ErrorIs(t, err, atypes.ErrNotFound)
		})
	}
}

func TestPrepareEpochRegistryRefusesAHeightInAPrunedEpoch(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)
	height := chain.firstHeightOf(1)
	require.NoError(t, chain.registry.PruneBefore(2))

	_, err := seedLayout(t, chain.genesis, committedCheckpoint(t, utils.Some(chain.registry.MarshalSnapshot())), height)
	require.ErrorIs(t, err, atypes.ErrPruned)
}

func TestPrepareEpochRegistryRefusesTheLastRoadOfAnEpoch(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)
	checkpoint := committedCheckpoint(t, utils.Some(chain.registry.MarshalSnapshot()))

	_, err := seedLayout(t, chain.genesis, checkpoint, chain.firstHeightOf(2)-1)
	require.ErrorIs(t, err, ErrClosingRoad)
}

func TestPrepareEpochRegistryRefusesARowWithTheGenesisEpochsOfAnotherChain(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)
	other := newTestEpochChain(t, rng)

	_, err := seedLayout(t, chain.genesis, committedCheckpoint(t, utils.Some(other.registry.MarshalSnapshot())), chain.firstHeightOf(2))
	require.Error(t, err)
}

func TestPrepareEpochRegistryAcceptsARowOfAnotherChainPrunedPastGenesis(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)
	other := newTestEpochChain(t, rng)
	height := other.firstHeightOf(2)
	require.NoError(t, other.registry.PruneBefore(2))

	layout, err := seedLayout(t, chain.genesis, committedCheckpoint(t, utils.Some(other.registry.MarshalSnapshot())), height)
	require.NoError(t, err)
	require.True(t, epoch.EpochEqual(other.registry.MustEpoch(3), openLayoutRegistry(t, chain.genesis, layout).MustEpoch(3)))
}

func TestPrepareEpochRegistryRejectsMalformedRow(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)

	_, err := seedLayout(t, chain.genesis, committedCheckpoint(t, utils.Some([]byte{0xff, 0x01})), chain.firstHeightOf(0))
	require.Error(t, err)
}

func TestPrepareEpochRegistryRejectsEmptyRow(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)

	_, err := seedLayout(t, chain.genesis, committedCheckpoint(t, utils.Some([]byte{})), chain.firstHeightOf(0))
	require.ErrorIs(t, err, ErrEmptyEpochRegistryRow)
}

func TestResolveEpochGenesisReadsTheGenesisValidators(t *testing.T) {
	conf := layoutConfig(t, t.TempDir())
	genDoc := genesisAt(1)
	genesis, err := ResolveEpochGenesis(conf, genDoc)
	require.NoError(t, err)
	require.Equal(t, genDoc, genesis.GenDoc)
	validator := makeValidator([]byte("layout-validator"), []byte("layout-node"), "localhost:26660")
	require.Equal(t, []atypes.PublicKey{validator.ValidatorKey}, genesis.Validators)
}
