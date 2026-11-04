package data

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestPushAppHash_StagesClosingEpochFromWindow(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	window := utils.OrPanic1(registry.Window(0, map[types.PublicKey]uint64{keys[1].Public(): 7}))
	source := NextEpochSource{
		Weights: map[types.PublicKey]uint64{keys[0].Public(): 9},
		Window:  utils.Some(window),
	}

	require.NoError(t, pushClosingAppHash(t, rng, registry, keys, source))
	require.Equal(t, utils.Some(window.Next()), registry.Pending())
}

func TestPushAppHash_StagesClosingEpochFromWeightsWhenWindowOpensAnotherEpoch(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	other, otherKeys := epoch.GenRegistryThrough(rng, 3, 2)
	later := utils.OrPanic1(other.Window(1, map[types.PublicKey]uint64{otherKeys[0].Public(): 7}))
	weights := map[types.PublicKey]uint64{keys[0].Public(): 9}
	fromWeights := utils.OrPanic1(registry.Window(0, weights)).Next()

	require.NoError(t, pushClosingAppHash(t, rng, registry, keys, NextEpochSource{Weights: weights, Window: utils.Some(later)}))
	require.Equal(t, utils.Some(fromWeights), registry.Pending())
}

// pushClosingAppHash pushes an AppHash for the last road of epoch 0 with source.
func pushClosingAppHash(t *testing.T, rng utils.Rng, registry *epoch.Registry, keys []types.SecretKey, source NextEpochSource) error {
	t.Helper()
	qc, blocks := commitQCAtRoad(registry.MustEpoch(0), keys, registry.LastRoad(0))
	store := newMemoryBlockStore(t)
	writeToBlockStore(t, store, []*types.FullCommitQC{qc}, [][]*types.Block{blocks})
	state := newTestState(t, &Config{Registry: registry}, store)
	return scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		if err := state.PushPartialBlock(ctx, qc, TestBlockMap(blocks)); err != nil {
			return err
		}
		return state.PushAppHashWithEpochSource(ctx, qc.Index(), types.GenAppHash(rng), source)
	})
}
