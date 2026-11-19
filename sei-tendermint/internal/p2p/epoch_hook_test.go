package p2p

import (
	"context"
	"testing"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// TestRegistryEpochHook pins that only a closing road derives an epoch, and that
// the next epoch's first road carries that epoch's committee. Deleting the
// ClosingEpoch guard makes the non-closing road return an error. Removing the
// wait makes the opening road return a range error instead of context.Canceled.
// Encoding epoch 1's committee instead of epoch 2's turns the committee assertion red.
func TestRegistryEpochHook(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	a, b := keys[0].Public(), keys[1].Public()
	hook := NewRegistryEpochHook(registry)
	opening := registry.LastRoad(1) + 1
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := hook.EpochState(canceled, opening)
	require.ErrorIs(t, err, context.Canceled)
	following, err := hook.EpochState(canceled, opening+1)
	require.NoError(t, err)
	require.False(t, following.IsPresent())

	past := registry.MustEpoch(1).RoadRange().First
	require.NoError(t, hook.RoadCommitted(past, []proxy.ValidatorUpdate{{PubKey: b, Power: 2}}))
	_, err = registry.Epoch(2)
	require.Error(t, err)

	require.NoError(t, hook.RoadCommitted(registry.LastRoad(0), []proxy.ValidatorUpdate{{PubKey: b, Power: 2}}))
	require.NoError(t, hook.RoadCommitted(registry.LastRoad(1), []proxy.ValidatorUpdate{
		{PubKey: a, Power: 3},
		{PubKey: b, Power: 2},
	}))
	require.NoError(t, registry.PruneBefore(1))

	state, err := hook.EpochState(t.Context(), registry.MustEpoch(2).RoadRange().First)
	require.NoError(t, err)
	got, ok := state.Get()
	require.True(t, ok)
	require.Equal(t, registry.TestPersisted(), got.EpochRegistry)
	require.Equal(t, hashable.MarshalCanonical(atypes.CommitteeConv.Encode(registry.MustEpoch(2).Committee())), got.CurrentCommittee)
	require.NotEqual(t, hashable.MarshalCanonical(atypes.CommitteeConv.Encode(registry.MustEpoch(1).Committee())), got.CurrentCommittee)

	none, err := hook.EpochState(t.Context(), registry.LastRoad(1))
	require.NoError(t, err)
	require.False(t, none.IsPresent())

	// A ready epoch state is returned even when ctx is already canceled.
	again, err := hook.EpochState(canceled, registry.MustEpoch(2).RoadRange().First)
	require.NoError(t, err)
	require.Equal(t, state, again)
}
