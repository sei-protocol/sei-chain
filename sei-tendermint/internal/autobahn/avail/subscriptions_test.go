package avail

import (
	"context"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestSubscribeLaneProposals_ErrLaneClosedAfterMapDrop(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	a, b := keys[0], keys[1]
	db := memblock.NewBlockDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ds := utils.OrPanic1(data.NewState(&data.Config{Registry: registry}, db))
	state := utils.OrPanic1(NewState(a, ds, utils.None[string]()))

	lane0 := state.LocalLane().OrPanic("genesis")
	want, err := state.ProduceLocalBlock(lane0, 0, types.GenPayload(rng))
	require.NoError(t, err)
	sub, err := state.SubscribeLaneProposals(lane0, 0)
	require.NoError(t, err)

	ep, err := registry.ActivateEpoch(
		map[types.PublicKey]uint64{b.Public(): 1},
		types.OpenRoadRange(), time.Time{}, registry.FirstBlock(),
	)
	require.NoError(t, err)
	state.ApplyEpoch(ep)

	// Wrong producer key is rejected even if that peer has a lane in the committee.
	otherLane := types.LaneID{Validator: b.Public(), Joined: ep.EpochIndex()}
	_, err = state.SubscribeLaneProposals(otherLane, 0)
	require.ErrorIs(t, err, ErrBadLane)

	got, err := sub.Recv(t.Context())
	require.NoError(t, err)
	require.Equal(t, want.Msg().Block().Header().Hash(), got.Msg().Block().Header().Hash())

	for inner, ctrl := range state.inner.Lock() {
		inner.dropLanes([]types.LaneID{lane0})
		ctrl.Updated()
	}
	_, err = sub.Recv(t.Context())
	require.ErrorIs(t, err, ErrLaneClosed)
}

func TestSubscribeLaneProposals_WrongValidator(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	a, b := keys[0], keys[1]
	db := memblock.NewBlockDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ds := utils.OrPanic1(data.NewState(&data.Config{Registry: registry}, db))
	state := utils.OrPanic1(NewState(a, ds, utils.None[string]()))

	_, err := state.SubscribeLaneProposals(types.LaneID{Validator: b.Public(), Joined: 0}, 0)
	require.ErrorIs(t, err, ErrBadLane)
}

// After a validator leaves, the old LaneID becomes closed at epochOfFirst; rejoining
// allocates a new LaneID. WaitLane skips the closed identity and Subscribe serves
// the new lane (StreamLaneProposals client path).
func TestWaitLane_LeaveRejoinNewLaneID(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	a, b := keys[0], keys[1]
	db := memblock.NewBlockDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ds := utils.OrPanic1(data.NewState(&data.Config{Registry: registry}, db))
	state := utils.OrPanic1(NewState(a, ds, utils.None[string]()))

	lane0 := state.LocalLane().OrPanic("genesis")
	got, err := state.WaitLane(ctx, a.Public(), utils.None[types.LaneID]())
	require.NoError(t, err)
	require.Equal(t, lane0, got)

	sub, err := state.SubscribeLaneProposals(lane0, 0)
	require.NoError(t, err)
	_, err = state.ProduceLocalBlock(lane0, 0, types.GenPayload(rng))
	require.NoError(t, err)
	_, err = sub.Recv(ctx)
	require.NoError(t, err)

	// Remove a from the committee. Closing-lane maps still serve until IsClosed.
	epLeave, err := registry.ActivateEpoch(
		map[types.PublicKey]uint64{b.Public(): 1},
		types.OpenRoadRange(), time.Time{}, registry.FirstBlock(),
	)
	require.NoError(t, err)
	state.ApplyEpoch(epLeave)
	require.False(t, state.LocalLane().IsPresent())

	// Dropping maps for the closed lane ends the stream with ErrLaneClosed.
	for inner, ctrl := range state.inner.Lock() {
		inner.dropLanes([]types.LaneID{lane0})
		ctrl.Updated()
	}
	_, err = sub.Recv(ctx)
	require.ErrorIs(t, err, ErrLaneClosed)

	// Client would WaitLane(..., exclude=lane0). LocalLane is None so it must not
	// return the closed identity; after rejoin it observes the new LaneID.
	var gotLane types.LaneID
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
		sc.Spawn(func() error {
			lane, err := state.WaitLane(ctx, a.Public(), utils.Some(lane0))
			if err != nil {
				return err
			}
			gotLane = lane
			return nil
		})
		epJoin, err := registry.ActivateEpoch(
			map[types.PublicKey]uint64{a.Public(): 1, b.Public(): 1},
			types.OpenRoadRange(), time.Time{}, registry.FirstBlock(),
		)
		if err != nil {
			return err
		}
		state.ApplyEpoch(epJoin)
		return nil
	}))
	lane1 := state.LocalLane().OrPanic("rejoin")
	require.NotEqual(t, lane0, lane1)
	require.Equal(t, a.Public(), lane1.Validator)
	require.Equal(t, lane1, gotLane)

	// Subscribe on the new LaneID works; the closed lane0 still matches the validator
	// key but Recv returns ErrLaneClosed because its map is gone.
	sub0, err := state.SubscribeLaneProposals(lane0, 0)
	require.NoError(t, err)
	_, err = sub0.Recv(ctx)
	require.ErrorIs(t, err, ErrLaneClosed)

	sub1, err := state.SubscribeLaneProposals(lane1, 0)
	require.NoError(t, err)
	want, err := state.ProduceLocalBlock(lane1, 0, types.GenPayload(rng))
	require.NoError(t, err)
	gotBlk, err := sub1.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, want.Msg().Block().Header().Hash(), gotBlk.Msg().Block().Header().Hash())
	require.Equal(t, lane1, gotBlk.Msg().Block().Header().Lane())
}
