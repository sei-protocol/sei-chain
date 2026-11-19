package node

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
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
	registry, err := p2p.NewEpochRegistry(genesis.GenDoc, slices.Values(validators), utils.None[[]byte]())
	require.NoError(t, err)
	require.NoError(t, registry.TryPushEpoch(registry.LastRoad(0), map[atypes.PublicKey]uint64{keys[0].Public(): 3, keys[1].Public(): 4}))
	require.NoError(t, registry.TryPushEpoch(registry.LastRoad(1), map[atypes.PublicKey]uint64{keys[2].Public(): 5}))
	return testEpochChain{genesis: genesis, registry: registry}
}

// firstHeightOf returns the first road height of epoch idx.
func (c testEpochChain) firstHeightOf(idx atypes.EpochIndex) int64 {
	return int64(c.registry.MustEpoch(idx).RoadRange().First) //nolint:gosec // test roads fit in an int64.
}

// rowAt returns the epoch registry row that the chain writes with the first road of epoch idx.
func (c testEpochChain) rowAt(t *testing.T, idx atypes.EpochIndex) []byte {
	t.Helper()
	state, err := c.registry.FirstRoadState(atypes.RoadIndex(c.firstHeightOf(idx))) //nolint:gosec // test roads are positive.
	require.NoError(t, err)
	return state.OrPanic("no first road state").Registry
}

// committedCheckpoint returns a FlatKV checkpoint dir whose state holds row as its epoch registry
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
			Name:      bootstrap.EpochStateModule,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte(bootstrap.EpochRegistryKey), Value: raw}}},
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

func requireSameEpoch(t *testing.T, want, got *atypes.Epoch) {
	t.Helper()
	require.Equal(t, want.EpochIndex(), got.EpochIndex())
	require.Equal(t, want.RoadRange(), got.RoadRange())
	require.True(t, want.Committee().Equal(got.Committee()))
}

func TestOpenCheckpointEpochsReadsTheEpochRegistryRow(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)
	checkpoint := committedCheckpoint(t, utils.Some(chain.rowAt(t, 2)))

	for name, height := range map[string]int64{
		"first road of epoch 2": chain.firstHeightOf(2),
		"last road of epoch 2":  chain.firstHeightOf(3) - 1,
	} {
		t.Run(name, func(t *testing.T) {
			epochs, err := OpenCheckpointEpochs(chain.genesis, checkpoint, height)
			require.NoError(t, err)
			for idx := atypes.EpochIndex(1); idx <= 3; idx++ {
				requireSameEpoch(t, chain.registry.MustEpoch(idx), epochs.registry.MustEpoch(idx))
			}
			_, err = epochs.registry.Epoch(0)
			require.ErrorIs(t, err, atypes.ErrPruned)
		})
	}
}

func TestOpenCheckpointEpochsWithoutRowHoldsTheGenesisEpochs(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)

	epochs, err := OpenCheckpointEpochs(chain.genesis, committedCheckpoint(t, utils.None[[]byte]()), chain.firstHeightOf(1)-1)
	require.NoError(t, err)
	requireSameEpoch(t, chain.registry.MustEpoch(1), epochs.registry.MustEpoch(1))
	_, err = epochs.registry.Epoch(2)
	require.ErrorIs(t, err, atypes.ErrNotFound)
}

func TestOpenCheckpointEpochsRefusesAHeightWithoutTheNextEpoch(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)
	for name, tc := range map[string]struct {
		row    utils.Option[[]byte]
		height int64
	}{
		"row of epoch 2, height in epoch 3": {utils.Some(chain.rowAt(t, 2)), chain.firstHeightOf(3)},
		"no row, height in epoch 1":         {utils.None[[]byte](), chain.firstHeightOf(1)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := OpenCheckpointEpochs(chain.genesis, committedCheckpoint(t, tc.row), tc.height)
			require.ErrorIs(t, err, atypes.ErrNotFound)
		})
	}
}

func TestOpenCheckpointEpochsRefusesAHeightInAPrunedEpoch(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)

	_, err := OpenCheckpointEpochs(chain.genesis, committedCheckpoint(t, utils.Some(chain.rowAt(t, 2))), chain.firstHeightOf(1)-1)
	require.ErrorIs(t, err, atypes.ErrPruned)
}

func TestOpenCheckpointEpochsRejectsMalformedRow(t *testing.T) {
	rng := utils.TestRng()
	chain := newTestEpochChain(t, rng)
	for name, row := range map[string][]byte{
		"not a registry": {0xff, 0x01},
		"empty":          {},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := OpenCheckpointEpochs(chain.genesis, committedCheckpoint(t, utils.Some(row)), chain.firstHeightOf(0))
			require.Error(t, err)
		})
	}
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
