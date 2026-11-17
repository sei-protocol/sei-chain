package epoch

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// fromSnapshot opens a registry from raw with genesis as its epoch 0.
func fromSnapshot(genesis *types.Epoch, raw []byte, stateDir utils.Option[string]) (*Registry, error) {
	return NewRegistryFromSnapshot(genesis.Committee(), genesis.RoadRange().First, time.Time{}, raw, stateDir)
}

// openGenesis opens a registry under dir with genesis as its epoch 0.
func openGenesis(genesis *types.Epoch, dir string) (*Registry, error) {
	return NewRegistry(genesis.Committee(), genesis.RoadRange().First, time.Time{}, utils.Some(dir))
}

// requireSourceEpochs requires registry to hold the live epochs of source, which are current-1
// through current+1.
func requireSourceEpochs(t *testing.T, source, registry *Registry, current types.EpochIndex) {
	t.Helper()
	for idx := current - 1; idx <= current+1; idx++ {
		require.True(t, EpochEqual(source.MustEpoch(idx), utils.OrPanic1(registry.Epoch(idx))))
	}
	if current >= 2 {
		_, err := registry.Epoch(current - 2)
		require.True(t, errors.Is(err, types.ErrPruned))
	}
	_, err := registry.Epoch(current + 2)
	require.True(t, errors.Is(err, types.ErrNotFound))
}

func TestNewRegistryFromSnapshotOpensWithSourceEpochs(t *testing.T) {
	rng := utils.TestRng()
	for _, current := range []types.EpochIndex{1, 2, 5} {
		source, keys := GenRegistryThrough(rng, 3, current)
		genesis := source.MustEpoch(0)
		require.NoError(t, source.TestPushEpoch(current-1, map[types.PublicKey]uint64{keys[0].Public(): 5}))
		require.NoError(t, source.PruneBefore(current-1))

		registry, err := fromSnapshot(genesis, source.MarshalSnapshot(), utils.None[string]())
		require.NoError(t, err)
		requireSourceEpochs(t, source, registry, current)
	}
}

func TestNewRegistryFromSnapshotPersistsTheSnapshotForNewRegistry(t *testing.T) {
	rng := utils.TestRng()
	source, keys := GenRegistryThrough(rng, 3, 5)
	genesis := source.MustEpoch(0)
	require.NoError(t, source.TestPushEpoch(4, map[types.PublicKey]uint64{keys[0].Public(): 5}))
	require.NoError(t, source.PruneBefore(4))
	dir := t.TempDir()
	older, err := openGenesis(genesis, dir)
	require.NoError(t, err)
	require.NoError(t, older.TestPushEpoch(0, map[types.PublicKey]uint64{keys[1].Public(): 5}))

	_, err = fromSnapshot(genesis, source.MarshalSnapshot(), utils.Some(dir))
	require.NoError(t, err)
	registry, err := openGenesis(genesis, dir)
	require.NoError(t, err)
	requireSourceEpochs(t, source, registry, 5)
}

func TestNewRegistryFromSnapshotRejectsTheGenesisEpochsOfAnotherChain(t *testing.T) {
	rng := utils.TestRng()
	source, _ := GenRegistryThrough(rng, 3, 2)
	other, _ := GenRegistryThrough(rng, 3, 2)

	_, err := fromSnapshot(other.MustEpoch(0), source.MarshalSnapshot(), utils.None[string]())
	require.Error(t, err)
}

func TestNewRegistryFromSnapshotPrunedPastGenesisOpensOnAnotherChainWithTheSameFirstRoad(t *testing.T) {
	rng := utils.TestRng()
	source, _ := GenRegistryThrough(rng, 3, 3)
	firstRoad := source.MustEpoch(0).RoadRange().First
	require.NoError(t, source.PruneBefore(2))
	otherCommittee, _ := types.GenCommittee(rng, 3)

	registry, err := NewRegistryFromSnapshot(
		otherCommittee, firstRoad, time.Time{}, source.MarshalSnapshot(), utils.None[string](),
	)
	require.NoError(t, err)
	require.True(t, EpochEqual(source.MustEpoch(3), utils.OrPanic1(registry.Epoch(3))))
}

func TestNewRegistryFromSnapshotRejectsMalformedValueWithoutWritingTheStateDir(t *testing.T) {
	rng := utils.TestRng()
	source, _ := GenRegistryThrough(rng, 3, 1)
	for name, raw := range map[string][]byte{
		"not protobuf":   {0xff, 0x01},
		"no live epochs": {},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			_, err := fromSnapshot(source.MustEpoch(0), raw, utils.Some(dir))
			require.Error(t, err)
			_, err = os.Stat(SnapshotDir(dir))
			require.True(t, errors.Is(err, os.ErrNotExist))
		})
	}
}
