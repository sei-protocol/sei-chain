package epoch

import (
	"errors"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// openGenesis opens a registry under dir with genesis as its epoch 0.
func openGenesis(genesis *types.Epoch, dir string) (*Registry, error) {
	return NewRegistry(genesis.Committee(), genesis.RoadRange().First, time.Time{}, utils.Some(dir))
}

// openGenesisOf opens a registry under dir with the genesis of source.
func openGenesisOf(source *Registry, dir string) (*Registry, error) {
	return openGenesis(source.MustEpoch(0), dir)
}

func TestRestoreSnapshotOpensWithSourceEpochs(t *testing.T) {
	rng := utils.TestRng()
	for _, current := range []types.EpochIndex{1, 2, 5} {
		source, keys := GenRegistryThrough(rng, 3, current)
		genesis := source.MustEpoch(0)
		require.NoError(t, source.TestPushEpoch(current-1, map[types.PublicKey]uint64{keys[0].Public(): 5}))
		require.NoError(t, source.PruneBefore(current-1))
		dir := t.TempDir()

		require.NoError(t, RestoreSnapshot(dir, utils.Some(source.MarshalSnapshot())))
		registry, err := openGenesis(genesis, dir)
		require.NoError(t, err)

		for idx := current - 1; idx <= current+1; idx++ {
			require.True(t, EpochEqual(source.MustEpoch(idx), utils.OrPanic1(registry.Epoch(idx))))
		}
		if current >= 2 {
			_, err := registry.Epoch(current - 2)
			require.True(t, errors.Is(err, types.ErrPruned))
		}
		_, err = registry.Epoch(current + 2)
		require.True(t, errors.Is(err, types.ErrNotFound))
	}
}

func TestRestoreSnapshotWithoutRowOpensGenesis(t *testing.T) {
	rng := utils.TestRng()
	source, _ := GenRegistryThrough(rng, 3, 3)
	dir := t.TempDir()
	require.NoError(t, RestoreSnapshot(dir, utils.Some(source.MarshalSnapshot())))

	require.NoError(t, RestoreSnapshot(dir, utils.None[[]byte]()))
	registry, err := openGenesisOf(source, dir)
	require.NoError(t, err)
	require.True(t, EpochEqual(source.MustEpoch(0), utils.OrPanic1(registry.Epoch(0))))
	require.True(t, EpochEqual(source.MustEpoch(1), utils.OrPanic1(registry.Epoch(1))))
	_, err = registry.Epoch(2)
	require.True(t, errors.Is(err, types.ErrNotFound))
}

func TestRestoreSnapshotWithGenesisEpochsOfAnotherChainDoesNotOpen(t *testing.T) {
	rng := utils.TestRng()
	source, _ := GenRegistryThrough(rng, 3, 2)
	other, _ := GenRegistryThrough(rng, 3, 2)
	dir := t.TempDir()

	require.NoError(t, RestoreSnapshot(dir, utils.Some(source.MarshalSnapshot())))
	_, err := openGenesisOf(other, dir)
	require.Error(t, err)
}

func TestRestoreSnapshotPrunedPastGenesisOpensOnAnotherChainWithTheSameFirstRoad(t *testing.T) {
	rng := utils.TestRng()
	source, _ := GenRegistryThrough(rng, 3, 3)
	firstRoad := source.MustEpoch(0).RoadRange().First
	require.NoError(t, source.PruneBefore(2))
	otherCommittee, _ := types.GenCommittee(rng, 3)
	dir := t.TempDir()

	require.NoError(t, RestoreSnapshot(dir, utils.Some(source.MarshalSnapshot())))
	registry, err := NewRegistry(otherCommittee, firstRoad, time.Time{}, utils.Some(dir))
	require.NoError(t, err)
	require.True(t, EpochEqual(source.MustEpoch(3), utils.OrPanic1(registry.Epoch(3))))
}

func TestRestoreSnapshotRejectsMalformedRow(t *testing.T) {
	for name, raw := range map[string][]byte{
		"not protobuf":   {0xff, 0x01},
		"no live epochs": {},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, RestoreSnapshot(t.TempDir(), utils.Some(raw)))
		})
	}
}
