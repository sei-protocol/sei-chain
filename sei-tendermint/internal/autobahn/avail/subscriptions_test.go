package avail

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

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

	// Stay: epoch 1 is already seeded at genesis with the same committee; advance
	// into it without ActivateEpoch (which must not rewrite existing epochs).
	var want1, got1 *types.Signed[*types.LaneProposal]
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBgNamed("runEpochAdvance", func() error {
			return utils.IgnoreCancel(state.runEpochAdvance(ctx))
		})
		sc.Spawn(func() error {
			var err error
			got1, err = sub.Recv(ctx)
			return err
		})
		if err := DriveAdvance(ctx, state, keys, 1); err != nil {
			return err
		}
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

	// Leave: peer drops from committee at epoch 2 (first vacant after genesis seeds).
	// Anchor-epoch prune drops closed lane maps (same path as runEvict) and ends the subscribe.
	epLeave, err := registry.ActivateEpoch(
		map[types.PublicKey]uint64{b.Public(): 1},
		time.Time{}, registry.FirstBlock(),
	)
	require.NoError(t, err)
	require.Equal(t, types.EpochIndex(2), epLeave.EpochIndex())
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBgNamed("runEpochAdvance", func() error {
			return utils.IgnoreCancel(state.runEpochAdvance(ctx))
		})
		return DriveAdvance(ctx, state, keys, epLeave.EpochIndex())
	}))
	require.False(t, state.LocalLane().IsPresent())

	for inner, ctrl := range state.inner.Lock() {
		require.Greater(t, inner.roads.next, inner.roads.first)
		tip := inner.roads.q[inner.roads.next-1].commitQC
		ep := inner.epoch.Load()
		require.Equal(t, epLeave.EpochIndex(), ep.EpochIndex())
		require.True(t, ep.IsClosed(lane0))
		n := inner.prune(data.Anchor{
			CommitQC: tip,
			AppQC:    data.TestAppQC([]types.SecretKey{b}, types.NewAppProposal(tip.Proposal(), types.AppHash{})),
			Epoch:    ep,
		})
		require.Equal(t, 1, n)
		ctrl.Updated()
	}
	_, err = sub.Recv(ctx)
	require.ErrorIs(t, err, ErrLaneClosed)

	// Rejoin: WaitForNextLane skips closed lane0 and observes the new LaneID.
	var gotLane types.LaneID
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBgNamed("runEpochAdvance", func() error {
			return utils.IgnoreCancel(state.runEpochAdvance(ctx))
		})
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
			time.Time{}, registry.FirstBlock(),
		)
		if err != nil {
			return err
		}
		return DriveAdvance(ctx, state, keys, epJoin.EpochIndex())
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

// Joiner catchup: a first-time joiner votes retained outstanding blocks; after
// leave/rejoin the cursor stays tip-aligned (no re-vote of already emitted
// headers; blocks produced while out are still voted once).
func TestJoinerCatchup_LaneVotes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 1)
		a := keys[0]
		b := types.GenSecretKey(rng)
		allKeys := append(keys, b)

		db := memblock.NewBlockDB()
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		ds := utils.OrPanic1(data.NewState(&data.Config{Registry: registry}, db))
		stateA := utils.OrPanic1(NewState(a, ds, utils.None[string]()))
		stateB := utils.OrPanic1(NewState(b, ds, utils.None[string]()))
		laneA := stateA.LocalLane().OrPanic("genesis")

		activate := func(weights map[types.PublicKey]uint64) *types.Epoch {
			ep, err := registry.ActivateEpoch(weights, time.Time{}, registry.FirstBlock())
			require.NoError(t, err)
			return ep
		}
		advance := func(want types.EpochIndex) {
			require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
				sc.SpawnBgNamed("runEpochAdvance", func() error {
					return utils.IgnoreCancel(stateB.runEpochAdvance(ctx))
				})
				return DriveAdvance(ctx, stateB, allKeys, want)
			}))
		}
		produce := func(n types.BlockNumber) *types.Signed[*types.LaneProposal] {
			prop, err := stateA.ProduceLocalBlock(laneA, n, types.GenPayload(rng))
			require.NoError(t, err)
			require.NoError(t, stateB.PushBlock(ctx, prop))
			stateB.setNextBlockToPersist(laneA, n+1)
			return prop
		}
		both := map[types.PublicKey]uint64{a.Public(): 1, b.Public(): 1}
		onlyA := map[types.PublicKey]uint64{a.Public(): 1}

		block0 := produce(0)
		epJoin := activate(both)
		advance(epJoin.EpochIndex())
		require.Equal(t, types.EpochIndex(2), stateB.LocalLane().OrPanic("joiner").Joined)

		sub := stateB.SubscribeLaneVotes()
		batch, err := sub.RecvBatch(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, len(batch))
		require.Equal(t, block0.Msg().Block().Header().Hash(), batch[0].Msg().Header().Hash())
		require.Equal(t, b.Public(), batch[0].Key())

		epLeave := activate(onlyA)
		advance(epLeave.EpochIndex())
		block1 := produce(1) // while out; skip RecvBatch so the cursor stays behind block1

		epRejoin := activate(both)
		advance(epRejoin.EpochIndex())
		require.Equal(t, types.EpochIndex(4), stateB.LocalLane().OrPanic("rejoiner").Joined)

		batch, err = sub.RecvBatch(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, len(batch))
		require.Equal(t, block1.Msg().Block().Header().Hash(), batch[0].Msg().Header().Hash(),
			"missed-while-out block is voted once; block0 must not be re-emitted")

		ctx2, cancel := context.WithCancel(ctx)
		defer cancel()
		done := false
		go func() {
			_, _ = sub.RecvBatch(ctx2)
			done = true
		}()
		synctest.Wait()
		require.False(t, done, "cursor must not rewind onto already-emitted headers")
		cancel()
		synctest.Wait()
		require.True(t, done)

		block2 := produce(2)
		batch, err = sub.RecvBatch(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, len(batch))
		require.Equal(t, block2.Msg().Block().Header().Hash(), batch[0].Msg().Header().Hash())
	})
}
