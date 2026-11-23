package evmonlyapp

import (
	"testing"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestChangesReplacesTheUncommittedEpoch(t *testing.T) {
	rng := utils.TestRng()
	firstKey := atypes.GenSecretKey(rng)
	secondKey := atypes.GenSecretKey(rng)
	weights := map[atypes.PublicKey]uint64{firstKey.Public(): 1}
	const first atypes.RoadHeight = 1
	const length atypes.RoadHeight = 10
	registry := utils.OrPanic1(atypes.GenesisRegistry(weights, first, length))
	epochs := &EpochRegistry{
		state:     utils.NewMutex(&epochRegistryState{registry: registry}),
		firstRoad: first,
		weights:   weights,
	}

	got, err := epochs.Changes(5)
	require.NoError(t, err)
	require.Empty(t, got)

	closing := first + length - 1
	firstWrite, err := epochs.Changes(closing)
	require.NoError(t, err)
	require.Len(t, firstWrite, 1)
	kept := epochs.Registry().Epochs()
	require.Len(t, kept, 3)
	require.False(t, kept[2].Committee().HasReplica(secondKey.Public()))

	epochs.weights = map[atypes.PublicKey]uint64{firstKey.Public(): 1, secondKey.Public(): 2}
	secondWrite, err := epochs.Changes(closing)
	require.NoError(t, err)
	require.NotEqual(t, firstWrite, secondWrite)
	window := epochs.Registry().Epochs()
	require.Len(t, window, 3)
	require.Equal(t, kept[0], window[0])
	require.Equal(t, kept[1], window[1])
	require.Equal(t, atypes.EpochIndex(2), window[2].EpochIndex())
	require.Equal(t, uint64(2), window[2].Committee().Weight(secondKey.Public()))

	got, err = epochs.Changes(closing + 1)
	require.NoError(t, err)
	require.Empty(t, got)

	nextClosing := first + 2*length - 1
	nextWrite, err := epochs.Changes(nextClosing)
	require.NoError(t, err)
	require.Len(t, nextWrite, 1)
	advanced := epochs.Registry().Epochs()
	require.Len(t, advanced, 3)
	require.Equal(t, window[1], advanced[0])
	require.Equal(t, window[2], advanced[1])
	require.Equal(t, atypes.EpochIndex(3), advanced[2].EpochIndex())
	require.Equal(t, uint64(2), advanced[1].Committee().Weight(secondKey.Public()))
}
