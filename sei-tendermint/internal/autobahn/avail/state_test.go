package avail

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func pushPeerLaneBlock(state *State, key types.SecretKey, payload *types.Payload) (*types.Signed[*types.LaneProposal], error) {
	lane := state.data.Anchor().Load().Epochs[0].Committee().Lane(key.Public()).OrPanic("lane")
	var b *types.Signed[*types.LaneProposal]
	for inner, ctrl := range state.inner.Lock() {
		ls, ok := inner.lanes[lane]
		if !ok {
			return nil, ErrLaneClosed
		}
		n := ls.blocks.next
		var parent types.BlockHeaderHash
		if ls.blocks.first < ls.blocks.next {
			parent = ls.blocks.q[ls.blocks.next-1].Msg().Block().Header().Hash()
		}
		b = types.Sign(key, types.NewLaneProposal(types.NewBlock(lane, n, parent, payload)))
		ls.blocks.pushBack(b)
		ctrl.Updated()
	}
	return b, nil
}

func persistTestLocalBlocks(t testing.TB, dir string, key types.SecretKey, blocks []*types.Block) {
	t.Helper()
	bp, _, err := persist.NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	for _, block := range blocks {
		lane := block.Header().Lane()
		if lane.Validator == key.Public() {
			require.NoError(t, bp.PruneAndPersist(lane, 0, []*types.Signed[*types.LaneProposal]{types.Sign(key, types.NewLaneProposal(block))}))
		}
	}
	require.NoError(t, bp.Close())
}

func nextRoad(s *State) types.RoadIndex {
	for inner := range s.inner.Lock() {
		return inner.roads.next
	}
	panic("unreachable")
}

type byLane[T any] map[types.LaneID][]T

func makeAppVotes(keys []types.SecretKey, proposal *types.AppProposal) []*types.Signed[*types.AppVote] {
	vote := types.NewAppVote(proposal)
	var votes []*types.Signed[*types.AppVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return votes
}

func makeLaneVotes(keys []types.SecretKey, h *types.BlockHeader) []*types.Signed[*types.LaneVote] {
	var votes []*types.Signed[*types.LaneVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewLaneVote(h)))
	}
	return votes
}

func pruneToHeader(state *State, keys []types.SecretKey, h *types.BlockHeader) {
	ep := state.ConsensusSpec().Load().Epoch
	qc := types.BuildCommitQC(ep, keys, utils.None[*types.CommitQC](), map[types.LaneID]*types.LaneQC{
		h.Lane(): types.NewLaneQC(makeLaneVotes(keys, h)),
	})
	for inner, ctrl := range state.inner.Lock() {
		inner.setAnchor(data.Anchor{First: utils.Some(data.AnchorFirst{
			CommitQC: qc,
			AppQC:    data.TestAppQC(keys, types.NewAppProposal(qc.View(), types.AppHash{})),
		}), Epochs: data.TestAnchor(ep).Epochs})
		ctrl.Updated()
	}
}

func qcPayloadHashes(qc *types.FullCommitQC) byLane[types.PayloadHash] {
	x := byLane[types.PayloadHash]{}
	for _, h := range qc.Headers() {
		x[h.Lane()] = append(x[h.Lane()], h.PayloadHash())
	}
	return x
}

func TestState(t *testing.T) {
	rng := utils.TestRng()
	for range 5 {
		testState(t, rng, utils.None[string]())
	}
}

// TestStateWithPersistence runs the same flow as TestState but with disk
// persistence enabled. The persist goroutine and prune (triggered by AppQC)
// run concurrently, exercising the cursor-clamp logic that prevents reading
// pruned map entries.
func TestStateWithPersistence(t *testing.T) {
	rng := utils.TestRng()
	for range 5 {
		testState(t, rng, utils.Some(t.TempDir()))
	}
}

// TestPrune_AnchorEpochDropsClosedLane checks that prune drops closing-lane
// maps when the Anchor epoch IsClosed them.
func TestPrune_AnchorEpochDropsClosedLane(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	a, b := keys[0], keys[1]

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
		ds := newTestDataState(t, &data.Config{Registry: registry})
		sc.SpawnBgNamed("data.Run", func() error { return utils.IgnoreCancel(ds.Run(ctx)) })

		ep0 := registry.MustEpoch(0)
		dir := t.TempDir()
		qc0, blocks0 := data.TestCommitQC(rng, ep0, keys, utils.None[*types.CommitQC]())
		if err := ds.PushPartialBlock(ctx, qc0, data.TestBlockMap(blocks0)); err != nil {
			return err
		}
		appHash0 := types.GenAppHash(rng)
		if err := ds.PushAppHash(ctx, qc0.Index(), appHash0, nil); err != nil {
			return err
		}
		if err := ds.PushAppQC(ctx, data.TestAppQC(keys, types.NewAppProposal(qc0.QC().View(), appHash0))); err != nil {
			return err
		}
		if _, err := ds.Anchor().Wait(ctx, func(anchor data.Anchor) bool {
			got, ok := anchor.CommitQC().Get()
			return ok && got.Index() == qc0.Index()
		}); err != nil {
			return err
		}

		persistTestLocalBlocks(t, dir, a, blocks0)
		state, err := NewState(a, ds, utils.Some(dir))
		if err != nil {
			return fmt.Errorf("NewState: %w", err)
		}

		defer func() { require.NoError(t, state.Close()) }()

		lane0 := state.LocalLane().OrPanic("genesis")
		sub := state.SubscribeLaneProposals(lane0, 0)

		if err := registry.TestPushEpoch(0, map[types.PublicKey]uint64{b.Public(): 1}); err != nil {
			return err
		}
		epLeave := registry.MustEpoch(2)
		ep1 := registry.MustEpoch(1)
		if err := TestAdvanceToEpoch(ctx, state, keys, registry, epLeave.EpochIndex()); err != nil {
			return err
		}

		for inner := range state.inner.Lock() {
			if _, ok := inner.lanes[lane0]; !ok {
				return fmt.Errorf("epoch advance must not drop closing-lane maps")
			}
		}

		// Construct a leave-epoch Anchor locally: data still holds only qc0, so the
		// first CommitQC of epoch 2 cannot be pushed without filling the road gap.
		prev := tipLink(ep1, keys[0], registry.LastRoad(1))
		qcLeave := types.BuildCommitQC(epLeave, []types.SecretKey{b}, utils.Some(prev), nil)
		anchor := data.Anchor{First: utils.Some(data.AnchorFirst{
			CommitQC: qcLeave,
			AppQC:    data.TestAppQC([]types.SecretKey{b}, types.NewAppProposal(qcLeave.View(), types.AppHash{})),
		}), Epochs: data.TestAnchor(epLeave).Epochs}

		for inner, ctrl := range state.inner.Lock() {
			if inner.roads.first < inner.roads.next {
				return fmt.Errorf("roads should still be empty after the certified epoch seal")
			}
			if _, ok := inner.lanes[lane0]; !ok {
				return fmt.Errorf("closing lane maps should still be present before anchor prune")
			}
			inner.setAnchor(anchor)
			if _, ok := inner.lanes[lane0]; ok {
				return fmt.Errorf("prune should have dropped closed lane0")
			}
			ctrl.Updated()
		}
		_, err = sub.Recv(ctx)
		if !errors.Is(err, ErrLaneClosed) {
			return fmt.Errorf("Subscribe Recv: got %v, want ErrLaneClosed", err)
		}
		return nil
	}))
}

func TestAnchorResetsState(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	epoch := registry.MustEpoch(0)
	var stateToClose *State
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		t.Logf("data.Run()")
		ds := newTestDataState(t, &data.Config{Registry: registry})
		s.SpawnBgNamed("data.Run", func() error { return utils.IgnoreCancel(ds.Run(ctx)) })

		t.Logf("Push FullCommitQC, blocks, AppHash, AppQC to data")
		qc, blocks := data.TestCommitQC(rng, epoch, keys, utils.None[*types.CommitQC]())
		if err := ds.PushPartialBlock(ctx, qc, data.TestBlockMap(blocks)); err != nil {
			return err
		}
		appHash := types.GenAppHash(rng)
		if err := ds.PushAppHash(ctx, qc.Index(), appHash, nil); err != nil {
			return err
		}
		appQC := data.TestAppQC(keys, types.NewAppProposal(qc.QC().View(), appHash))
		if err := ds.PushAppQC(ctx, appQC); err != nil {
			return err
		}

		t.Logf("wait for anchor to be updated")
		if _, err := ds.Anchor().Wait(ctx, func(anchor data.Anchor) bool {
			a, ok := anchor.CommitQC().Get()
			return ok && a.Index() == qc.Index()
		}); err != nil {
			return err
		}

		t.Logf("NewState() should load the anchor")
		dir := t.TempDir()
		persistTestLocalBlocks(t, dir, keys[0], blocks)
		state, err := NewState(keys[0], ds, utils.Some(dir))
		if err != nil {
			return fmt.Errorf("NewState(): %w", err)
		}
		stateToClose = state
		if err := utils.TestDiff(utils.Some(qc.QC()), state.ConsensusSpec().Load().CommitQC); err != nil {
			return err
		}
		t.Logf("avail.Run()")
		s.SpawnBgNamed("avail.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })

		t.Logf("push next CommitQC to avail")
		qc, _ = data.TestCommitQC(rng, epoch, keys, utils.Some(qc.QC()))
		if err := state.PushCommitQC(ctx, qc.QC()); err != nil {
			return err
		}
		t.Logf("wait for this CommitQC to be persisted in avail")
		_, err = state.ConsensusSpec().Wait(ctx, func(spec types.ConsensusSpec) bool {
			gotQC, ok := spec.CommitQC.Get()
			return ok && gotQC.Index() == qc.Index()
		})
		return err
	}))
	require.NoError(t, stateToClose.Close())
}

func testState(t *testing.T, rng utils.Rng, stateDir utils.Option[string]) {
	t.Helper()
	ctx := t.Context()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)
	committee := ep0.Committee()

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		ds := newTestDataState(t, &data.Config{Registry: registry})
		s.SpawnBgNamed("ds.Run()", func() error { return utils.IgnoreCancel(ds.Run(ctx)) })
		state, err := NewState(keys[0], ds, stateDir)
		if err != nil {
			return fmt.Errorf("NewState(): %w", err)
		}
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })

		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)

			t.Logf("Push some blocks.")
			want := byLane[types.PayloadHash]{}
			for range 10 {
				key := keys[rng.Intn(len(keys))]
				lane := committee.Lane(key.Public()).OrPanic("lane")
				p := types.GenPayload(rng)
				want[lane] = append(want[lane], p.Hash())
				b, err := pushPeerLaneBlock(state, key, p)
				if err != nil {
					return fmt.Errorf("pushPeerLaneBlock(): %w", err)
				}
				if err := utils.TestDiff(b.Msg().Block().Payload(), p); err != nil {
					return fmt.Errorf("snapshot: %w", err)
				}
			}

			t.Logf("Push votes for all the blocks.")
			for lane := range committee.Lanes().All() {
				next := state.NextBlock(lane)
				for i := types.LaneRangeOpt(prev, lane).Next(); i < next; i++ {
					b, err := state.Block(ctx, lane, i)
					if err != nil {
						return fmt.Errorf("state.TryBlock(): %w", err)
					}
					for _, vote := range makeLaneVotes(keys, b.Msg().Block().Header()) {
						if err := state.PushVote(ctx, vote); err != nil {
							return fmt.Errorf("state.PushVote(): %w", err)
						}
					}
				}
			}

			t.Logf("Push a commit QC.")
			laneQCs, err := state.WaitForLaneQCs(ctx, ep0, prev)
			if err != nil {
				return fmt.Errorf("state.WaitForNewLaneQCs(): %w", err)
			}
			qc := types.BuildCommitQC(ep0, keys, prev, laneQCs)
			if err := state.PushCommitQC(ctx, qc); err != nil {
				return fmt.Errorf("state.PushCommitQC(): %w", err)
			}
			prev = utils.Some(qc)

			t.Logf("Check that a CommitQC was successfully reconstructed.")
			got, err := state.fullCommitQC(ctx, qc.Index())
			if err != nil {
				return fmt.Errorf("state.fullCommitQC(): %w", err)
			}
			if err := utils.TestDiff(want, qcPayloadHashes(got)); err != nil {
				return fmt.Errorf("snapshot: %w", err)
			}

			t.Logf("Push app votes.")
			appHash := types.GenAppHash(rng)
			appProposal := types.NewAppProposal(qc.View(), appHash)
			if err := ds.PushAppHash(ctx, appProposal.Index(), appHash, nil); err != nil {
				return fmt.Errorf("ds.PushAppHash(): %w", err)
			}
			for _, vote := range makeAppVotes(keys, appProposal) {
				if err := state.PushAppVote(ctx, vote); err != nil {
					return fmt.Errorf("state.PushAppVote(): %w", err)
				}
			}

			t.Logf("Executed CommitQC should be eventually evicted")
			for inner, ctrl := range state.inner.Lock() {
				if err := ctrl.WaitUntil(ctx, func() bool { return inner.roads.first == appProposal.Index()+1 }); err != nil {
					return err
				}
			}

			t.Logf("Check that the executed local blocks have been pruned")
			for lane := range committee.Lanes().All() {
				if lr := qc.LaneRange(lane); lr.Next() > 0 {
					if _, err := state.Block(ctx, lane, lr.Next()-1); !errors.Is(err, types.ErrPruned) {
						return fmt.Errorf("state.Block(): %w, want %v", err, types.ErrPruned)
					}
				}
			}

			t.Logf("Check that the blocks were successfully pushed to data state.")
			b, err := ds.Block(ctx, types.LaneBlockPos{Index: got.Index()})
			if err != nil {
				return fmt.Errorf("ds.Block(): %w", err)
			}
			if err := utils.TestDiff(b.Header(), got.Headers()[0]); err != nil {
				return fmt.Errorf("snapshot: %w", err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreInnerNextEpoch(t *testing.T) {
	t.Run("genesis anchor survives registry pruning", func(t *testing.T) {
		rng := utils.TestRng()
		registry, _ := epoch.GenRegistry(rng, 3)
		ds := newTestDataState(t, &data.Config{Registry: registry})
		i, err := newInner(ds.Anchor().Load(), ds.GenesisTimestamp(), &loadedState{})
		require.NoError(t, err)
		require.Equal(t, registry.MustEpoch(0), i.applied())
		require.NoError(t, registry.PruneBefore(1))
		restored, err := newInner(ds.Anchor().Load(), ds.GenesisTimestamp(), &loadedState{})
		require.NoError(t, err)
		require.Equal(t, i.applied(), restored.applied())
	})
	for _, idx := range []types.EpochIndex{0, 1} {
		t.Run(fmt.Sprintf("anchor at last road of epoch %d", idx), func(t *testing.T) {
			rng := utils.TestRng()
			registry, keys := epoch.GenRegistryThrough(rng, 3, 2)
			last := registry.LastRoad(idx)
			ds := newTestDataStateAtRoad(t, rng, registry, keys, last)
			i, err := newInner(ds.Anchor().Load(), ds.GenesisTimestamp(), &loadedState{})
			require.NoError(t, err)
			spec := i.consensusSpec.Load()
			cqc, ok := spec.CommitQC.Get()
			require.True(t, ok)
			require.Equal(t, last, cqc.Index(), "must not walk tip back to LastRoad(0)-1")
			require.Equal(t, idx+1, spec.Epoch.EpochIndex())
		})
	}
}

// TestStateRestartFromPersisted runs the state with persistence through 2
// iterations (blocks → votes → commitQC → appQC each), stops, and restarts
// from the same directory. This verifies that what the runtime persist
// goroutine writes can be correctly loaded back by loadPersistedState/newInner.
//
// The restarted state uses a fresh in-memory data.State, so this covers the
// availability persisters themselves: CommitQCs and local blocks are loaded back
// and lane next-block cursors resume where they left off.
func TestStateRestartFromPersisted(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep0 := registry.MustEpoch(0)
	committee := ep0.Committee()
	dir := t.TempDir()

	// Phase 1: Run state with persistence through 2 iterations.
	var wantAppQCIdx types.RoadIndex
	var wantNextBlocks map[types.LaneID]types.BlockNumber
	// Hoisted so phase 1's WALs can be closed after its goroutines have stopped: the lane and
	// commitQC WALs hold an exclusive lock on their directories, which phase 2 reopens.
	var state1 *State

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		ds := newTestDataState(t, &data.Config{Registry: registry})
		s.SpawnBgNamed("data.Run", func() error {
			return utils.IgnoreCancel(ds.Run(ctx))
		})
		state, err := NewState(keys[0], ds, utils.Some(dir))
		if err != nil {
			return err
		}
		state1 = state
		s.SpawnBgNamed("avail.Run", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		var prev utils.Option[*types.CommitQC]
		for i := range 2 {
			t.Logf("iteration %d", i)

			for range 5 {
				key := keys[rng.Intn(len(keys))]
				if _, err := pushPeerLaneBlock(state, key, types.GenPayload(rng)); err != nil {
					return fmt.Errorf("pushPeerLaneBlock: %w", err)
				}
			}

			for lane := range committee.Lanes().All() {
				next := state.NextBlock(lane)
				for n := types.LaneRangeOpt(prev, lane).Next(); n < next; n++ {
					b, err := state.Block(ctx, lane, n)
					if err != nil {
						return fmt.Errorf("Block(%v,%d): %w", lane, n, err)
					}
					for _, vote := range makeLaneVotes(keys, b.Msg().Block().Header()) {
						if err := state.PushVote(ctx, vote); err != nil {
							return fmt.Errorf("PushVote: %w", err)
						}
					}
				}
			}

			laneQCs, err := state.WaitForLaneQCs(ctx, ep0, prev)
			if err != nil {
				return fmt.Errorf("WaitForLaneQCs: %w", err)
			}
			qc := types.BuildCommitQC(ep0, keys, prev, laneQCs)
			if err := state.PushCommitQC(ctx, qc); err != nil {
				return fmt.Errorf("PushCommitQC: %w", err)
			}

			appProposal := types.NewAppProposal(qc.View(), types.GenAppHash(rng))
			for _, vote := range makeAppVotes(keys, appProposal) {
				if err := state.PushAppVote(ctx, vote); err != nil {
					return fmt.Errorf("PushAppVote: %w", err)
				}
			}
			if _, err := state.appQC(ctx, appProposal.Index()); err != nil {
				return fmt.Errorf("WaitForAppQC: %w", err)
			}
			prev = utils.Some(qc)
			wantAppQCIdx = appProposal.Index()
		}

		// Wait for commitQC persistence. markCommitQCsPersisted fires after
		// all commitQCs in the batch are on disk. Block goroutines may still
		// be in flight, but scope.Parallel in runPersist ensures they complete
		// before the next batch, so the data is durable by scope exit.
		if _, err := state.ConsensusSpec().Wait(ctx, func(spec types.ConsensusSpec) bool {
			commitQC, ok := spec.CommitQC.Get()
			return ok && commitQC.Index()+1 > wantAppQCIdx
		}); err != nil {
			return fmt.Errorf("waitForCommitQC: %w", err)
		}

		wantNextBlocks = make(map[types.LaneID]types.BlockNumber, committee.Lanes().Len())
		for lane := range committee.Lanes().All() {
			wantNextBlocks[lane] = state.NextBlock(lane)
		}
		return nil
	}))

	// Phase 2: Restart from the same directory. scope.Run has stopped avail.Run, so nothing is
	// writing to phase 1's WALs and they can be released.
	require.NoError(t, state1.Close())

	ds2 := newTestDataState(t, &data.Config{Registry: registry})
	state2, err := NewState(keys[0], ds2, utils.Some(dir))
	require.NoError(t, err)

	_, ok := state2.ConsensusSpec().Load().CommitQC.Get()
	require.True(t, ok, "ConsensusSpec should contain a CommitQC after restart")

	for lane := range committee.Lanes().All() {
		require.Equal(t, wantNextBlocks[lane], state2.NextBlock(lane),
			"NextBlock(%v) should match pre-restart value", lane)
	}
}

func TestPushBlockRejectsBadParentHash(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	ds := newTestDataState(t, &data.Config{Registry: registry})
	state := utils.OrPanic1(NewState(keys[0], ds, utils.Some(t.TempDir())))

	committee := registry.MustEpoch(0).Committee()
	lane := committee.Lane(keys[0].Public()).OrPanic("lane")
	// Produce a valid first block on our lane.
	_, err := state.ProduceLocalBlock(lane, state.NextBlock(lane), types.GenPayload(rng))
	require.NoError(t, err)

	// Create a second block with a fake parentHash.
	fakeBlock := types.NewBlock(lane, 1, types.GenBlockHeaderHash(rng), types.GenPayload(rng))
	fakeProp := types.Sign(keys[0], types.NewLaneProposal(fakeBlock))

	// Producer equivocation is rejected with an error.
	require.Error(t, state.PushBlock(ctx, fakeProp))
	// Queue did not advance — the bad block was dropped.
	require.Equal(t, types.BlockNumber(1), state.NextBlock(lane))
}

func TestProduceLocalBlockAfterPruningUsesZeroParentHash(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	producer := utils.OrPanic1(NewState(keys[0], newTestDataState(t, &data.Config{Registry: registry}), utils.None[string]()))
	t.Cleanup(func() { require.NoError(t, producer.Close()) })
	lane := producer.LocalLane().OrPanic("local lane")
	for range 3 {
		_, err := producer.ProduceLocalBlock(lane, producer.NextBlock(lane), types.GenPayload(rng))
		require.NoError(t, err)
	}
	lastNumber := producer.NextBlock(lane) - 1
	last, err := producer.Block(ctx, lane, lastNumber)
	require.NoError(t, err)

	pruneToHeader(producer, keys, last.Msg().Block().Header())
	qc := producer.ConsensusSpec().Load().CommitQC.OrPanic("finalized lane")
	require.Equal(t, producer.NextBlock(lane), qc.LaneRange(lane).Next())
	for n := range lastNumber + 1 {
		_, err := producer.Block(ctx, lane, n)
		require.ErrorIs(t, err, types.ErrPruned)
	}

	next, err := producer.ProduceLocalBlock(lane, producer.NextBlock(lane), types.GenPayload(rng))
	require.NoError(t, err)
	require.Equal(t, lastNumber+1, next.Msg().Block().Header().BlockNumber())
	require.Equal(t, types.BlockHeaderHash{}, next.Msg().Block().Header().ParentHash())
	child, err := producer.ProduceLocalBlock(lane, producer.NextBlock(lane), types.GenPayload(rng))
	require.NoError(t, err)
	require.Equal(t, next.Msg().Block().Header().Hash(), child.Msg().Block().Header().ParentHash())
}

func TestSetNextBlockToPersistDoesNotRewind(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	state := utils.OrPanic1(NewState(
		keys[0],
		newTestDataState(t, &data.Config{Registry: registry}),
		utils.None[string](),
	))
	lane := registry.MustEpoch(0).Committee().Lane(keys[0].Public()).OrPanic("lane")

	state.setNextBlockToPersist(lane, 5)
	state.setNextBlockToPersist(lane, 3)
	for inner := range state.inner.Lock() {
		require.Equal(t, types.BlockNumber(5), inner.lanes[lane].nextBlockToPersist)
	}
	state.setNextBlockToPersist(lane, 6)
	for inner := range state.inner.Lock() {
		require.Equal(t, types.BlockNumber(6), inner.lanes[lane].nextBlockToPersist)
	}
}

func TestPushBlockRejectsWrongSigner(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	ds := newTestDataState(t, &data.Config{Registry: registry})
	state := utils.OrPanic1(NewState(keys[0], ds, utils.Some(t.TempDir())))

	lane := registry.MustEpoch(0).Committee().Lane(keys[0].Public()).OrPanic("lane")
	// Create a block on keys[0]'s lane but sign it with keys[1].
	block := types.NewBlock(lane, 0, types.GenBlockHeaderHash(rng), types.GenPayload(rng))
	prop := types.Sign(keys[1], types.NewLaneProposal(block))

	err := state.PushBlock(ctx, prop)
	require.Error(t, err)
}

func TestNewStateRestoresLaneBehindCommit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		persisted int
		local     bool
	}{
		{name: "missing local blocks", local: true},
		{name: "truncated local blocks", persisted: 1, local: true},
		{name: "local blocks cover commit", persisted: 2, local: true},
		{name: "local blocks ahead of commit", persisted: 3, local: true},
		{name: "missing remote blocks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rng := utils.TestRng()
			registry, keys := epoch.GenRegistry(rng, 2)
			ep := registry.MustEpoch(0)
			lane := ep.Committee().Lane(keys[0].Public()).OrPanic("lane")
			loaded := contiguousBlocks(keys[0], lane, 3, rng)
			blocks := make([]*types.Block, len(loaded))
			for n, b := range loaded {
				blocks[n] = b.Proposal.Msg().Block()
			}
			qc := types.BuildCommitQC(ep, keys, utils.None[*types.CommitQC](), map[types.LaneID]*types.LaneQC{
				lane: data.TestLaneQC(keys, blocks[1].Header()),
			})
			dir := t.TempDir()
			persistTestLocalBlocks(t, dir, keys[0], blocks[:tc.persisted])
			cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
			require.NoError(t, err)
			require.NoError(t, cp.PruneAndPersist(ep.RoadRange().First, []*types.CommitQC{qc}))
			require.NoError(t, cp.Close())
			key := keys[1]
			if tc.local {
				key = keys[0]
			}
			ds := newTestDataState(t, &data.Config{Registry: registry})
			state, err := NewState(key, ds, utils.Some(dir))
			require.NoError(t, err)
			require.Equal(t, types.BlockNumber(tc.persisted), state.NextBlock(lane))
			require.Equal(t, utils.Some(qc), state.ConsensusSpec().Load().CommitQC)
			require.NoError(t, state.Close())
		})
	}
}

func TestNewStateAcceptsMissingLocalParent(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 1)
	qc, blocks := data.TestCommitQC(rng, registry.MustEpoch(0), keys, utils.None[*types.CommitQC]())
	store := blockstore.NewTestStore(t)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.WriteBlock(types.NewRoadBlock(qc, blocks)))
	app := types.NewAppProposal(qc.QC().View(), types.GenAppHash(rng))
	require.NoError(t, store.WriteAppProposal(app))
	require.NoError(t, store.WriteAppQC(data.TestAppQC(keys, app)))
	require.NoError(t, store.Flush())
	ds, err := data.NewState(&data.Config{Registry: registry}, store)
	require.NoError(t, err)
	dir := t.TempDir()
	state, err := NewState(keys[0], ds, utils.Some(dir))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, state.Close()) })
	lane := state.LocalLane().OrPanic("local lane")
	require.Equal(t, qc.QC().LaneRange(lane).Next(), state.NextBlock(lane))
	proposal, err := state.ProduceLocalBlock(lane, state.NextBlock(lane), types.GenPayload(rng))
	require.NoError(t, err)
	require.Equal(t, types.BlockHeaderHash{}, proposal.Msg().Block().Header().ParentHash())
}

func TestNewStateWithPersistence(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	ep0 := registry.MustEpoch(0)
	t.Run("empty dir loads fresh state", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(t, &data.Config{Registry: registry})

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		// Road queues start at the registry's first road.
		require.Equal(t, ep0.RoadRange().First, state.First())
	})

	t.Run("loads persisted blocks", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(t, &data.Config{Registry: registry})
		lane := ep0.Committee().Lane(keys[0].Public()).OrPanic("lane")

		// Persist blocks using BlockPersister.
		bp, _, err := persist.NewBlockPersister(utils.Some(dir))
		require.NoError(t, err)

		var parent types.BlockHeaderHash
		for n := range types.BlockNumber(3) {
			block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
			signed := types.Sign(keys[0], types.NewLaneProposal(block))
			parent = block.Header().Hash()
			require.NoError(t, bp.PruneAndPersist(
				lane,
				0,
				[]*types.Signed[*types.LaneProposal]{signed},
			))
		}

		// Release the seeding persister's WAL locks before NewState opens the same directory.
		require.NoError(t, bp.Close())

		// Now construct state — it should load the blocks.
		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		require.Equal(t, types.BlockNumber(3), state.NextBlock(lane))
	})

	t.Run("loads persisted commitQCs", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(t, &data.Config{Registry: registry})

		// Persist CommitQCs to disk.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)

		qcs := make([]*types.CommitQC, 3)
		prev := utils.None[*types.CommitQC]()
		for i := range qcs {
			qcs[i] = types.BuildCommitQC(ep0, keys, prev, nil)
			prev = utils.Some(qcs[i])
			require.NoError(t, cp.PruneAndPersist(ep0.RoadRange().First, []*types.CommitQC{qcs[i]}))
		}

		// Release the seeding persister's WAL locks before NewState opens the same directory.
		require.NoError(t, cp.Close())

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		// All 3 commitQCs should be loaded (no AppQC to skip past).
		require.Equal(t, ep0.RoadRange().First, state.First())
		// ConsensusSpec should contain the last loaded CommitQC.
		require.NoError(t, utils.TestDiff(utils.Some(qcs[2]), state.ConsensusSpec().Load().CommitQC))
	})

	t.Run("non-contiguous commitQC files return error", func(t *testing.T) {
		dir := t.TempDir()

		// Build 6 sequential CommitQCs from the registry's first road.
		allQCs := make([]*types.CommitQC, 6)
		prev := utils.None[*types.CommitQC]()
		for i := range allQCs {
			allQCs[i] = types.BuildCommitQC(ep0, keys, prev, nil)
			prev = utils.Some(allQCs[i])
		}

		// Persist the first three QCs contiguously, then try to skip to the sixth.
		// Persist enforces strict sequential order, so the gap
		// is caught at write time rather than at load time.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		for i := range 3 {
			require.NoError(t, cp.PruneAndPersist(ep0.RoadRange().First, []*types.CommitQC{allQCs[i]}))
		}
		err = cp.PruneAndPersist(ep0.RoadRange().First, []*types.CommitQC{allQCs[5]})
		require.Error(t, err)
		require.Contains(t, err.Error(), "out of sequence")
		require.NoError(t, cp.Close())
	})
}

// TestHeaders_WaitsForPrevEpochLaneVote checks that after the applied epoch
// advances, a LaneVote that only verifies under the Anchor (prev) committee is
// still ingested into byKey and unblocks headers() for a prior-epoch LaneRange.
func TestHeaders_WaitsForPrevEpochLaneVote(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		stay := types.GenSecretKey(rng)
		leaver := types.GenSecretKey(rng)
		a := types.GenSecretKey(rng)
		b := types.GenSecretKey(rng)

		genesis, err := types.NewCommittee(map[types.PublicKey]uint64{
			stay.Public(): 1, leaver.Public(): 1, a.Public(): 1, b.Public(): 1,
		})
		require.NoError(t, err)
		registry, err := epoch.NewRegistry(genesis, 0, time.Time{}, utils.None[string]())
		require.NoError(t, err)
		ds := newTestDataState(t, &data.Config{Registry: registry})
		state, err := NewState(stay, ds, utils.None[string]())
		require.NoError(t, err)

		require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {

			ep0 := registry.MustEpoch(0)
			lane := ep0.Committee().Lane(stay.Public()).OrPanic("stay lane")
			header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, &types.Payload{}).Header()
			leaverVote := types.Sign(leaver, types.NewLaneVote(header))

			if err := registry.TestPushEpoch(0, map[types.PublicKey]uint64{stay.Public(): 1, a.Public(): 1, b.Public(): 1}); err != nil {
				return err
			}
			epLeave := registry.MustEpoch(2)
			keys := []types.SecretKey{stay, leaver, a, b}
			if err := TestAdvanceToEpoch(ctx, state, keys, registry, epLeave.EpochIndex()); err != nil {
				return err
			}

			for inner := range state.inner.Lock() {
				if inner.applied().EpochIndex() != epLeave.EpochIndex() {
					return fmt.Errorf("applied epoch = %d, want %d", inner.applied().EpochIndex(), epLeave.EpochIndex())
				}
				ae := inner.anchor.Epochs[0]
				if ae.EpochIndex() >= epLeave.EpochIndex() {
					return fmt.Errorf("anchor epoch = %d, want < %d", ae.EpochIndex(), epLeave.EpochIndex())
				}
			}
			if leaverVote.Msg().Verify(epLeave.Committee()) == nil && epLeave.Committee().HasReplica(leaverVote.Key()) {
				return fmt.Errorf("leaver must fail under applied")
			}
			if leaverVote.Msg().Verify(ep0.Committee()) != nil || !ep0.Committee().HasReplica(leaverVote.Key()) {
				return fmt.Errorf("leaver must pass under anchor")
			}

			lr := types.NewLaneRange(lane, 0, utils.Some(header))
			var got []*types.BlockHeader
			var herr error
			done := false
			sc.Spawn(func() error {
				got, herr = state.headers(ctx, ep0, lr)
				done = true
				return nil
			})
			synctest.Wait()
			if done {
				return fmt.Errorf("headers should wait for a matching LaneVote")
			}

			if err := state.PushVote(ctx, leaverVote); err != nil {
				return err
			}
			synctest.Wait()
			if !done {
				return fmt.Errorf("headers did not complete after LaneVote")
			}
			if herr != nil {
				return herr
			}
			if len(got) != 1 {
				return fmt.Errorf("headers len = %d, want 1", len(got))
			}
			if got[0].Hash() != header.Hash() {
				return fmt.Errorf("header hash mismatch")
			}
			return nil
		}))
	})
}

func TestPushCommitQC_MidEpochNoWait(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	state := utils.OrPanic1(NewState(keys[0], newTestDataState(t, &data.Config{Registry: registry}), utils.None[string]()))
	// Anchor: LastRoad(0). Spec: epoch 1's first road, already in epoch 1.
	require.NoError(t, TestAdvanceToEpoch(t.Context(), state, keys, registry, 1))
	ep := registry.MustEpoch(1)
	// Epoch 2 is missing, but accepting a CommitQC inside epoch 1 must not wait for it.
	prev := testAnchorAt(registry.MustEpoch(0), ep, keys, registry.LastRoad(0)).CommitQC()
	qc := types.BuildCommitQC(ep, keys, prev, nil)
	require.Equal(t, ep.RoadRange().First, qc.Index())
	leader := ep.Committee().Leader(qc.View()).ED25519().Address().String()
	commits := gathered(t, "tendermint_internal_autobahn_consensus_commits", map[string]string{"leader": leader})
	require.NoError(t, state.PushCommitQC(t.Context(), qc))
	require.Equal(t, commits+1, gathered(t, "tendermint_internal_autobahn_consensus_commits", map[string]string{"leader": leader}))
	// Admission advances the road buffer to epoch 1's first road + 1, before persistence.
	require.Equal(t, ep.RoadRange().First+1, nextRoad(state))
}

func TestPushCommitQC_StaleAfterAdvanceSoftDrops(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	ds := newTestDataState(t, &data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	ep1 := registry.MustEpoch(1)
	require.NoError(t, TestAdvanceToEpoch(t.Context(), state, keys, registry, ep1.EpochIndex()))

	ep0 := registry.MustEpoch(0)
	before := nextRoad(state)
	qc0 := types.BuildCommitQC(ep0, keys, utils.None[*types.CommitQC](), nil)
	require.Equal(t, types.EpochIndex(0), qc0.View().EpochIndex)
	require.NoError(t, state.PushCommitQC(t.Context(), qc0))
	require.Equal(t, before, nextRoad(state))
}

func TestAdvanceRequiresAnchorAndSeal(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	ep0 := types.NewEpoch(0, types.RoadRange{First: 1, Next: 3}, registry.MustEpoch(0).Committee())
	ep1 := types.NewEpoch(1, types.RoadRange{First: 3, Next: 5}, registry.MustEpoch(1).Committee())
	ep2 := types.NewEpoch(2, types.RoadRange{First: 5, Next: 7}, registry.MustEpoch(1).Committee())
	// epochs: [1,3),[3,5),[5,7). qc0 through qc3 cover roads 1 through 4.
	qc0 := types.BuildCommitQC(ep0, keys, utils.None[*types.CommitQC](), nil)
	qc1 := types.BuildCommitQC(ep0, keys, utils.Some(qc0), nil)
	qc2 := types.BuildCommitQC(ep1, keys, utils.Some(qc1), nil)
	qc3 := types.BuildCommitQC(ep1, keys, utils.Some(qc2), nil)
	anchor := data.Anchor{Epochs: [2]*types.Epoch{ep0, ep1}}
	i, err := newInner(anchor, registry.GenesisTimestamp(), &loadedState{commitQCs: []*types.CommitQC{qc0, qc1, qc2, qc3}})
	require.NoError(t, err)
	// Road 4 seals epoch 1, but the spec cannot advance to 5 while epoch 2 is missing.
	require.Equal(t, ep1, i.applied())
	require.Equal(t, utils.Some(qc2), i.consensusSpec.Load().CommitQC)
	// advance should be a noop while epoch 2 is missing.
	spec := i.consensusSpec.Load()
	i.advance()
	require.Equal(t, spec, i.consensusSpec.Load())
	// Moving the anchor to road 3 exposes epoch 2; persisted qc3 now justifies spec 5.
	anchor = data.Anchor{Epochs: [2]*types.Epoch{ep1, ep2}, First: utils.Some(data.AnchorFirst{CommitQC: qc2, AppQC: data.TestAppQC(keys, types.NewAppProposal(qc2.View(), nil))})}
	i.setAnchor(anchor)
	require.Equal(t, ep2, i.applied())
	require.Equal(t, utils.Some(qc3), i.consensusSpec.Load().CommitQC)
	// With the same anchor but no persisted qc3, the spec stays at road 4 in epoch 1.
	beforeSeal, err := newInner(anchor, registry.GenesisTimestamp(), &loadedState{})
	require.NoError(t, err)
	require.Equal(t, ep1, beforeSeal.applied())
	require.Equal(t, utils.Some(qc2), beforeSeal.consensusSpec.Load().CommitQC)
}

// anchorWalk is the Anchor sequence data publishes as it certifies the last
// road of epochs 0 through 2. Build it before the registry drops those epochs.
func anchorWalk(t *testing.T, registry *epoch.Registry, keys []types.SecretKey) []data.Anchor {
	t.Helper()
	var anchors []data.Anchor
	for m := types.EpochIndex(0); m <= 2; m++ {
		ep := registry.MustEpoch(m)
		last := registry.LastRoad(m)
		qc := types.BuildCommitQC(ep, keys, utils.Some(tipLink(ep, keys[0], last-1)), nil)
		require.Equal(t, last, qc.Index())
		anchors = append(anchors, data.Anchor{First: utils.Some(data.AnchorFirst{
			CommitQC: qc,
			AppQC:    data.TestAppQC(keys, types.NewAppProposal(qc.View(), types.AppHash{})),
		}), Epochs: [2]*types.Epoch{ep, registry.MustEpoch(m + 1)}})
	}
	return anchors
}

// pruneToAnchors replays anchors through inner.setAnchor the way runEvict does, and
// returns the last CommitQC.
func pruneToAnchors(state *State, anchors []data.Anchor) *types.CommitQC {
	for _, a := range anchors {
		for inner, ctrl := range state.inner.Lock() {
			inner.setAnchor(a)
			ctrl.Updated()
		}
	}
	return anchors[len(anchors)-1].CommitQC().OrPanic("anchor")
}

func requireSpec(t *testing.T, state *State, qc *types.CommitQC, idx types.EpochIndex) {
	t.Helper()
	spec := state.ConsensusSpec().Load()
	require.Equal(t, qc.Index()+1, spec.Index())
	require.Equal(t, idx, spec.Epoch.EpochIndex())
}

func TestPruneAdvancesConsensusSpec(t *testing.T) {
	for _, withTip := range []bool{false, true} {
		t.Run(fmt.Sprint(withTip), func(t *testing.T) {
			rng := utils.TestRng()
			registry, keys := epoch.GenRegistryThrough(rng, 3, 3)
			state := utils.OrPanic1(NewState(keys[0], newTestDataState(t, &data.Config{Registry: registry}), utils.None[string]()))
			if withTip {
				qc := types.BuildCommitQC(registry.MustEpoch(0), keys, utils.None[*types.CommitQC](), nil)
				require.NoError(t, state.PushCommitQC(t.Context(), qc))
				state.markCommitQCsPersisted(qc)
			}
			anchors := anchorWalk(t, registry, keys)
			qc := pruneToAnchors(state, anchors[:2])
			requireSpec(t, state, qc, 2)
			require.NoError(t, registry.PruneBefore(3))
			qc = pruneToAnchors(state, anchors[2:])
			requireSpec(t, state, qc, 3)
		})
	}
}

func TestMarkCommitQCsPersistedDoesNotRewindSpecAfterPrune(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistryThrough(rng, 2, 3)
	ds := newTestDataState(t, &data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)
	qc0 := types.BuildCommitQC(registry.MustEpoch(0), keys, utils.None[*types.CommitQC](), nil)
	require.NoError(t, state.PushCommitQC(t.Context(), qc0))
	state.markCommitQCsPersisted(qc0)
	qc := pruneToAnchors(state, anchorWalk(t, registry, keys))
	spec := state.ConsensusSpec().Load()
	require.Equal(t, utils.Some(qc), spec.CommitQC)
	state.markCommitQCsPersisted(qc0)
	require.Equal(t, spec, state.ConsensusSpec().Load())
}
