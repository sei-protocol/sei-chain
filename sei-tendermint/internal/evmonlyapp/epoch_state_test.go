package evmonlyapp

import (
	"errors"
	"testing"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// testEpochRegistry returns a registry of epochs of length roads from first, configured with weights.
func testEpochRegistry(weights map[atypes.PublicKey]uint64, first, length atypes.RoadHeight) *EpochRegistry {
	registry := utils.OrPanic1(atypes.GenesisRegistry(weights, first, length))
	return &EpochRegistry{
		state:     utils.NewMutex(&epochRegistryState{registry: registry}),
		firstRoad: first,
		weights:   weights,
	}
}

// staked returns a staking read that returns weights.
func staked(weights map[atypes.PublicKey]uint64) func() (map[atypes.PublicKey]uint64, error) {
	return func() (map[atypes.PublicKey]uint64, error) { return weights, nil }
}

func TestChangesReplacesTheUncommittedEpoch(t *testing.T) {
	rng := utils.TestRng()
	firstKey := atypes.GenSecretKey(rng)
	secondKey := atypes.GenSecretKey(rng)
	weights := map[atypes.PublicKey]uint64{firstKey.Public(): 1}
	const first atypes.RoadHeight = 1
	const length atypes.RoadHeight = 10
	epochs := testEpochRegistry(weights, first, length)

	got, err := epochs.Changes(5, staked(nil))
	require.NoError(t, err)
	require.Empty(t, got)

	closing := first + length - 1
	firstWrite, err := epochs.Changes(closing, staked(nil))
	require.NoError(t, err)
	require.Len(t, firstWrite, 1)
	kept := epochs.Registry().Epochs()
	require.Len(t, kept, 3)
	require.False(t, kept[2].Committee().HasReplica(secondKey.Public()))

	secondWrite, err := epochs.Changes(closing, staked(map[atypes.PublicKey]uint64{firstKey.Public(): 1, secondKey.Public(): 2}))
	require.NoError(t, err)
	require.NotEqual(t, firstWrite, secondWrite)
	window := epochs.Registry().Epochs()
	require.Len(t, window, 3)
	require.Equal(t, kept[0], window[0])
	require.Equal(t, kept[1], window[1])
	require.Equal(t, atypes.EpochIndex(2), window[2].EpochIndex())
	require.Equal(t, uint64(2), window[2].Committee().Weight(secondKey.Public()))

	got, err = epochs.Changes(closing+1, staked(nil))
	require.NoError(t, err)
	require.Empty(t, got)

	nextClosing := first + 2*length - 1
	nextWrite, err := epochs.Changes(nextClosing, staked(nil))
	require.NoError(t, err)
	require.Len(t, nextWrite, 1)
	advanced := epochs.Registry().Epochs()
	require.Len(t, advanced, 3)
	require.Equal(t, window[1], advanced[0])
	require.Equal(t, window[2], advanced[1])
	require.Equal(t, atypes.EpochIndex(3), advanced[2].EpochIndex())
	require.Equal(t, uint64(2), advanced[1].Committee().Weight(secondKey.Public()))
}

func TestChangesCommitsAClosingRoadsEpochToStakingsWeights(t *testing.T) {
	rng := utils.TestRng()
	configured := atypes.GenSecretKey(rng).Public()
	stakedKey := atypes.GenSecretKey(rng).Public()
	epochs := testEpochRegistry(map[atypes.PublicKey]uint64{configured: 1}, 1, 3)
	reads := 0
	read := func() (map[atypes.PublicKey]uint64, error) {
		reads++
		return map[atypes.PublicKey]uint64{stakedKey: 7}, nil
	}

	for road := atypes.RoadHeight(1); road < 3; road++ {
		_, err := epochs.Changes(road, read)
		require.NoError(t, err)
	}
	require.Zero(t, reads, "only a closing road reads staking")
	_, err := epochs.Changes(3, read)
	require.NoError(t, err)
	require.Equal(t, 1, reads)
	window := epochs.Registry().Epochs()
	require.Equal(t, atypes.EpochIndex(2), window[2].EpochIndex())
	require.Equal(t, uint64(7), window[2].Committee().Weight(stakedKey))
	require.False(t, window[2].Committee().HasReplica(configured))
	require.True(t, window[1].Committee().HasReplica(configured), "the genesis epochs keep the configured validators")
}

func TestChangesFailsWhenStakingCannotBeRead(t *testing.T) {
	rng := utils.TestRng()
	epochs := testEpochRegistry(map[atypes.PublicKey]uint64{atypes.GenSecretKey(rng).Public(): 1}, 1, 3)
	before := epochs.Registry()
	errUnreadable := errors.New("unreadable")
	_, err := epochs.Changes(3, func() (map[atypes.PublicKey]uint64, error) { return nil, errUnreadable })
	require.ErrorIs(t, err, errUnreadable)
	require.Equal(t, before, epochs.Registry(), "the window does not advance")
}

func TestClosingEpochIsTheLastRoadOfARetainedEpoch(t *testing.T) {
	rng := utils.TestRng()
	const first, length atypes.RoadHeight = 7, 3
	epochs := testEpochRegistry(map[atypes.PublicKey]uint64{atypes.GenSecretKey(rng).Public(): 1}, first, length)
	none := utils.None[atypes.EpochIndex]()
	require.Equal(t, none, epochs.ClosingEpoch(first-1), "a road before the first")
	require.Equal(t, none, epochs.ClosingEpoch(first))
	require.Equal(t, utils.Some[atypes.EpochIndex](0), epochs.ClosingEpoch(first+length-1))
	require.Equal(t, utils.Some[atypes.EpochIndex](1), epochs.ClosingEpoch(first+2*length-1))
	require.Equal(t, none, epochs.ClosingEpoch(first+3*length-1), "epoch 2 is not derived yet")

	for road := first; road < first+length; road++ {
		_, err := epochs.Changes(road, staked(nil))
		require.NoError(t, err)
	}
	require.Equal(t, utils.Some[atypes.EpochIndex](2), epochs.ClosingEpoch(first+3*length-1))
}
