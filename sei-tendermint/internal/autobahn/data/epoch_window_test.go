package data

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestOpeningEpochWindow(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := testCommittee(rng)
	weights := map[types.PublicKey]uint64{keys[1].Public(): 7}
	registry := utils.OrPanic1(epoch.NewRegistry(committee, 0, time.Time{}, utils.None[string]()))
	state := &State{cfg: &Config{Registry: registry}}

	window, err := state.OpeningEpochWindow(registry.LastRoad(0), weights)
	require.NoError(t, err)
	require.Equal(t, utils.None[epoch.Window](), window)

	window, err = state.OpeningEpochWindow(registry.LastRoad(0)+1, weights)
	require.NoError(t, err)
	w, ok := window.Get()
	require.True(t, ok)
	require.Equal(t, types.EpochIndex(1), w.Current().EpochIndex())
	require.Equal(t, types.EpochIndex(2), w.Next().EpochIndex())
}

func TestStageClosingEpochUsesWindow(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := testCommittee(rng)
	weights := map[types.PublicKey]uint64{keys[1].Public(): 7}
	registry := utils.OrPanic1(epoch.NewRegistry(committee, 0, time.Time{}, utils.None[string]()))
	state := &State{cfg: &Config{Registry: registry}}
	window := utils.OrPanic1(registry.Window(0, weights))

	err := state.stageClosingEpoch(registry.LastRoad(0), NextEpochSource{Window: utils.Some(window)})
	require.NoError(t, err)
	require.Equal(t, utils.Some(window.Next()), registry.Pending())
}

func testCommittee(rng utils.Rng) (*types.Committee, []types.SecretKey) {
	keys := utils.GenSliceN(rng, 3, types.GenSecretKey)
	weights := map[types.PublicKey]uint64{}
	for i, key := range keys {
		weights[key.Public()] = uint64(i + 1)
	}
	return utils.OrPanic1(types.NewCommittee(weights)), keys
}
