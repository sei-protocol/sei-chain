package avail

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
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
	db := utils.OrPanic1(blockstore.New(memblock.NewBlockDB()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ds := utils.OrPanic1(data.NewState(&data.Config{Registry: registry}, db))
	state := utils.OrPanic1(NewState(a, ds, utils.None[string]()))

	lane0 := state.LocalLane().OrPanic("genesis")
	want, err := state.ProduceLocalBlock(lane0, 0, types.GenPayload(rng))
	require.NoError(t, err)
	sub := state.SubscribeLaneProposals(lane0, 0)

	ep, err := registry.ActivateEpoch(
		map[types.PublicKey]uint64{b.Public(): 1},
		types.OpenRoadRange(), time.Time{}, registry.FirstBlock(),
	)
	require.NoError(t, err)
	state.ApplyEpoch(ep)

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

func TestSubscribeLaneProposals_WrongValidatorPanics(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	a, b := keys[0], keys[1]
	db := utils.OrPanic1(blockstore.New(memblock.NewBlockDB()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ds := utils.OrPanic1(data.NewState(&data.Config{Registry: registry}, db))
	state := utils.OrPanic1(NewState(a, ds, utils.None[string]()))

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	state.SubscribeLaneProposals(types.LaneID{Validator: b.Public(), Joined: 0}, 0)
}

func TestSubscribeLaneProposals_StayLeaveRejoin(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	a, b := keys[0], keys[1]
	peer := a.Public()
	db := utils.OrPanic1(blockstore.New(memblock.NewBlockDB()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ds := utils.OrPanic1(data.NewState(&data.Config{Registry: registry}, db))
	state := utils.OrPanic1(NewState(a, ds, utils.None[string]()))

	lane0 := state.LocalLane().OrPanic("genesis")
	got, err := state.WaitForNextLane(ctx, peer, utils.None[types.LaneID]())
	require.NoError(t, err)
	require.Equal(t, lane0, got)

	sub := state.SubscribeLaneProposals(lane0, 0)
	want0, err := state.ProduceLocalBlock(lane0, 0, types.GenPayload(rng))
	require.NoError(t, err)
	got0, err := sub.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, want0.Msg().Block().Header().Hash(), got0.Msg().Block().Header().Hash())

	// Stay while Recv waits for the next block: ApplyEpoch must not end the subscribe.
	var want1, got1 *types.Signed[*types.LaneProposal]
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
		sc.Spawn(func() error {
			var err error
			got1, err = sub.Recv(ctx)
			return err
		})
		epStay, err := registry.ActivateEpoch(
			map[types.PublicKey]uint64{a.Public(): 1, b.Public(): 1},
			types.OpenRoadRange(), time.Time{}, registry.FirstBlock(),
		)
		if err != nil {
			return err
		}
		state.ApplyEpoch(epStay)
		if cur := state.LocalLane().OrPanic("stay"); cur != lane0 {
			return fmt.Errorf("stay LocalLane = %v, want %v", cur, lane0)
		}
		want1, err = state.ProduceLocalBlock(lane0, 1, types.GenPayload(rng))
		return err
	}))
	require.Equal(t, want1.Msg().Block().Header().Hash(), got1.Msg().Block().Header().Hash())
	require.Equal(t, lane0, got1.Msg().Block().Header().Lane())
	got, err = state.WaitForNextLane(ctx, peer, utils.None[types.LaneID]())
	require.NoError(t, err)
	require.Equal(t, lane0, got)

	// Leave: peer drops from committee; map drop ends the subscribe.
	epLeave, err := registry.ActivateEpoch(
		map[types.PublicKey]uint64{b.Public(): 1},
		types.OpenRoadRange(), time.Time{}, registry.FirstBlock(),
	)
	require.NoError(t, err)
	state.ApplyEpoch(epLeave)
	require.False(t, state.LocalLane().IsPresent())

	for inner, ctrl := range state.inner.Lock() {
		inner.dropLanes([]types.LaneID{lane0})
		ctrl.Updated()
	}
	_, err = sub.Recv(ctx)
	require.ErrorIs(t, err, ErrLaneClosed)

	// Rejoin: WaitForNextLane skips closed lane0 and observes the new LaneID.
	var gotLane types.LaneID
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
		sc.Spawn(func() error {
			lane, err := state.WaitForNextLane(ctx, peer, utils.Some(lane0))
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
	require.Equal(t, peer, lane1.Validator)
	require.Equal(t, lane1, gotLane)

	sub0 := state.SubscribeLaneProposals(lane0, 0)
	_, err = sub0.Recv(ctx)
	require.ErrorIs(t, err, ErrLaneClosed)

	sub1 := state.SubscribeLaneProposals(lane1, 0)
	want, err := state.ProduceLocalBlock(lane1, 0, types.GenPayload(rng))
	require.NoError(t, err)
	gotBlk, err := sub1.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, want.Msg().Block().Header().Hash(), gotBlk.Msg().Block().Header().Hash())
	require.Equal(t, lane1, gotBlk.Msg().Block().Header().Lane())
}
