package avail

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
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
	ds := newTestDataState(t, &data.Config{Registry: registry})
	state := utils.OrPanic1(NewState(a, ds, utils.None[string]()))

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	state.SubscribeLaneProposals(types.LaneID{Validator: b.Public(), Joined: 0}, 0)
}

func TestSubscribeLaneProposals_WaitsForPersistence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 1)
		state := utils.OrPanic1(NewState(keys[0], newTestDataState(t, &data.Config{Registry: registry}), utils.None[string]()))
		defer func() { require.NoError(t, state.Close()) }()
		lane := state.LocalLane().OrPanic("genesis")
		var blocks []*types.Signed[*types.LaneProposal]
		for n := range types.LaneHeight(10) {
			b, err := state.ProduceLocalBlock(lane, n, types.GenPayload(rng))
			require.NoError(t, err)
			blocks = append(blocks, b)
		}
		for _, persisted := range []types.LaneHeight{0, 3, 7} {
			sub := state.SubscribeLaneProposals(lane, 0)
			received := make([]*types.Signed[*types.LaneProposal], 0, len(blocks))
			require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
				sc.SpawnBg(func() error {
					for {
						b, err := sub.Recv(ctx)
						if err != nil {
							return utils.IgnoreCancel(err)
						}
						received = append(received, b)
					}
				})
				state.setNextBlockToPersist(lane, persisted)
				synctest.Wait()
				return nil
			}))
			require.NoError(t, utils.TestDiff(blocks[:persisted], received))
		}
	})
}

func TestSubscribeLaneProposals_StayLeaveRejoin(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	a, b := keys[0], keys[1]
	peer := a.Public()
	ds := newTestDataState(t, &data.Config{Registry: registry})
	state := utils.OrPanic1(NewState(a, ds, utils.None[string]()))

	lane0 := state.LocalLane().OrPanic("genesis")
	got, err := state.WaitForNextLane(ctx, peer, utils.None[types.LaneID]())
	require.NoError(t, err)
	require.Equal(t, lane0, got)

	sub := state.SubscribeLaneProposals(lane0, 0)
	want0, err := state.ProduceLocalBlock(lane0, 0, types.GenPayload(rng))
	require.NoError(t, err)
	state.setNextBlockToPersist(lane0, 1)
	got0, err := sub.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, want0.Msg().Block().Header().Hash(), got0.Msg().Block().Header().Hash())

	// Stay: epoch 1 is already seeded at genesis with the same committee.
	var want1, got1 *types.Signed[*types.LaneProposal]
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
		sc.Spawn(func() error {
			var err error
			got1, err = sub.Recv(ctx)
			return err
		})
		if err := TestAdvanceToEpoch(ctx, state, keys, registry, 1); err != nil {
			return err
		}
		if cur := state.LocalLane().OrPanic("stay"); cur != lane0 {
			return fmt.Errorf("stay LocalLane = %v, want %v", cur, lane0)
		}
		want1, err = state.ProduceLocalBlock(lane0, 1, types.GenPayload(rng))
		state.setNextBlockToPersist(lane0, 2)
		return err
	}))
	require.Equal(t, want1.Msg().Block().Header().Hash(), got1.Msg().Block().Header().Hash())
	require.Equal(t, lane0, got1.Msg().Block().Header().Lane())
	got, err = state.WaitForNextLane(ctx, peer, utils.None[types.LaneID]())
	require.NoError(t, err)
	require.Equal(t, lane0, got)

	// Leave: peer drops from committee at epoch 2 (first vacant after genesis seeds).
	// Anchor-epoch prune drops closed lane maps (same path as runEvict) and ends the subscribe.
	require.NoError(t, registry.TestPushEpoch(0, map[types.PublicKey]uint64{b.Public(): 1}))
	epLeave := registry.MustEpoch(2)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
		return TestAdvanceToEpoch(ctx, state, keys, registry, epLeave.EpochIndex())
	}))
	require.False(t, state.LocalLane().IsPresent())

	require.NoError(t, registry.TestPushEpoch(1, map[types.PublicKey]uint64{a.Public(): 1, b.Public(): 1}))
	epJoin := registry.MustEpoch(3)
	prev := testAnchorAt(registry.MustEpoch(1), epLeave, keys, registry.LastRoad(1)).CommitQC()
	firstLeave := types.BuildCommitQC(epLeave, []types.SecretKey{b}, prev, nil)
	require.NoError(t, state.PushCommitQC(ctx, firstLeave))
	state.markCommitQCsPersisted(firstLeave)

	for inner, ctrl := range state.inner.Lock() {
		require.Greater(t, inner.roads.next, inner.roads.first)
		tip := inner.roads.q[inner.roads.next-1].commitQC
		ep := inner.applied()
		require.Equal(t, epLeave.EpochIndex(), ep.EpochIndex())
		require.True(t, ep.IsClosed(lane0))
		inner.setAnchor(data.Anchor{First: utils.Some(data.AnchorFirst{
			CommitQC: tip,
			AppQC:    data.TestAppQC([]types.SecretKey{b}, types.NewAppProposal(tip.View(), types.AppHash{})),
		}), Epochs: [2]*types.Epoch{ep, epJoin}})
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
		return TestAdvanceToEpoch(ctx, state, keys, registry, epJoin.EpochIndex())
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
	state.setNextBlockToPersist(lane1, 1)
	gotBlk, err := sub1.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, want.Msg().Block().Header().Hash(), gotBlk.Msg().Block().Header().Hash())
	require.Equal(t, lane1, gotBlk.Msg().Block().Header().Lane())
}

func TestLaneRevoteOnEpochAdvance(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 1)
	state := utils.OrPanic1(NewState(keys[0], newTestDataState(t, &data.Config{Registry: registry}), utils.None[string]()))
	defer func() { _ = state.Close() }()
	lane := state.LocalLane().OrPanic("lane")
	var headers []*types.LaneBlockHeader
	// Produce 3 blocks.
	for n := range types.LaneHeight(3) {
		block := utils.OrPanic1(state.ProduceLocalBlock(lane, n, types.GenPayload(rng)))
		headers = append(headers, block.Msg().Block().Header())
	}
	// Persist 2.
	state.setNextBlockToPersist(lane, 2)
	// Receive 2 blocks.
	sub := state.SubscribeLaneVotes()
	requireLaneVotes(t, ctx, sub,
		types.Sign(keys[0], types.NewLaneVote(0, headers[0])),
		types.Sign(keys[0], types.NewLaneVote(0, headers[1])),
	)

	// Prune 1st, advance epoch, receive 2nd again.
	pruneToHeader(state, keys, headers[0])
	utils.OrPanic(TestAdvanceToEpoch(ctx, state, keys, registry, 1))
	requireLaneVotes(t, ctx, sub,
		types.Sign(keys[0], types.NewLaneVote(1, headers[1])),
	)

	// Persist 3rd and receive it.
	state.setNextBlockToPersist(lane, 3)
	requireLaneVotes(t, ctx, sub,
		types.Sign(keys[0], types.NewLaneVote(1, headers[2])),
	)
}

func TestSubscribeLaneVotes_PreviousAndCurrentEpochs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		voter  int
		epochs []types.EpochIndex
	}{
		{"staying validator", 0, []types.EpochIndex{1, 2}},
		{"departing validator", 1, []types.EpochIndex{1}},
		{"joining validator", 2, []types.EpochIndex{2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := t.Context()
				rng := utils.TestRng()
				registry, keys := epoch.GenRegistry(rng, 2)
				keys = append(keys, types.GenSecretKey(rng))
				require.NoError(t, registry.TestPushEpoch(0, map[types.PublicKey]uint64{keys[0].Public(): 1, keys[2].Public(): 1}))
				state := utils.OrPanic1(NewState(keys[tc.voter], newTestDataState(t, &data.Config{Registry: registry}), utils.None[string]()))
				defer func() { require.NoError(t, state.Close()) }()
				lane := registry.MustEpoch(0).Committee().Lane(keys[0].Public()).OrPanic("staying lane")
				first := testSignedBlock(keys[0], lane, 0, types.LaneBlockHeaderHash{}, rng)
				second := testSignedBlock(keys[0], lane, 1, first.Msg().Block().Header().Hash(), rng)
				require.NoError(t, state.PushBlock(ctx, first))
				require.NoError(t, state.PushBlock(ctx, second))
				state.setNextBlockToPersist(lane, 1)
				require.NoError(t, TestAdvanceToEpoch(ctx, state, keys, registry, 2))

				expected := func(headers ...*types.LaneBlockHeader) []*types.Signed[*types.LaneVote] {
					var votes []*types.Signed[*types.LaneVote]
					for _, epochIndex := range tc.epochs {
						for _, header := range headers {
							votes = append(votes, types.Sign(keys[tc.voter], types.NewLaneVote(epochIndex, header)))
						}
					}
					return votes
				}
				sub := state.SubscribeLaneVotes()
				requireLaneVotes(t, ctx, sub, expected(first.Msg().Block().Header())...)
				state.setNextBlockToPersist(lane, 2)
				if tc.voter != 1 {
					requireLaneVotes(t, ctx, sub, types.Sign(keys[tc.voter], types.NewLaneVote(2, second.Msg().Block().Header())))
				}

				require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
					sc.SpawnBg(func() error {
						_, err := sub.RecvBatch(ctx)
						if err == nil {
							return fmt.Errorf("votes must not be re-emitted within the same epoch")
						}
						return utils.IgnoreCancel(err)
					})
					synctest.Wait()
					return nil
				}))
				requireLaneVotes(t, ctx, state.SubscribeLaneVotes(), expected(first.Msg().Block().Header(), second.Msg().Block().Header())...)
			})
		})
	}
}

func requireLaneVotes(t *testing.T, ctx context.Context, sub *LaneVotesRecv, want ...*types.Signed[*types.LaneVote]) {
	t.Helper()
	var got []*types.Signed[*types.LaneVote]
	for len(got) < len(want) {
		batch, err := sub.RecvBatch(ctx)
		require.NoError(t, err)
		got = append(got, batch...)
	}
	require.Equal(t, want, got)
}

func TestBlock_WaitForFutureLane(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 1)
	joiner := types.GenSecretKey(rng)
	allKeys := append(keys, joiner)
	require.NoError(t, registry.TestPushEpoch(0, map[types.PublicKey]uint64{keys[0].Public(): 1, joiner.Public(): 1}))
	require.NoError(t, registry.TestPushEpoch(1, map[types.PublicKey]uint64{keys[0].Public(): 1}))
	require.NoError(t, registry.TestPushEpoch(2, map[types.PublicKey]uint64{keys[0].Public(): 1}))
	ep1, ep2 := registry.MustEpoch(1), registry.MustEpoch(2)
	ep3, ep4 := registry.MustEpoch(3), registry.MustEpoch(4)
	lane := ep2.Committee().Lane(joiner.Public()).OrPanic("joining lane")
	for _, tc := range []struct {
		name    string
		anchor  data.Anchor
		errWant error
	}{
		{"block", testAnchorAt(ep1, ep2, allKeys, ep1.RoadRange().Next-1), nil},
		// Skip beyond the lane's membership without ever admitting its block.
		{"pruned", testAnchorAt(ep3, ep4, allKeys, ep3.RoadRange().First), types.ErrPruned},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rng := utils.TestRng()
				state := utils.OrPanic1(NewState(joiner, newTestDataState(t, &data.Config{Registry: registry}), utils.None[string]()))
				defer func() { require.NoError(t, state.Close()) }()
				want := types.Sign(joiner, types.NewLaneProposal(types.NewLaneBlock(lane, 0, types.LaneBlockHeaderHash{}, types.GenPayload(rng))))
				require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
					// PushBlock should succeed in both cases.
					sc.Spawn(func() error { return state.PushBlock(ctx, want) })
					sc.Spawn(func() error {
						// Block should fail if the epoch is jumped over.
						got, err := state.Block(ctx, lane, 0)
						if !errors.Is(err, tc.errWant) {
							return fmt.Errorf("state.Block(): %v, want %v", err, tc.errWant)
						}
						if err != nil {
							return nil
						}
						return utils.TestDiff(want, got)
					})
					synctest.Wait()
					for inner, ctrl := range state.inner.Lock() {
						inner.setAnchor(tc.anchor)
						ctrl.Updated()
					}
					return nil
				}))
			})
		})
	}
}
