package avail

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/stretchr/testify/require"
)

func pushPeerLaneBlock(state *State, key types.SecretKey, payload *types.Payload) (*types.Signed[*types.LaneProposal], error) {
	lane := state.data.Registry().MustEpoch(0).Committee().Lane(key.Public()).OrPanic("lane")
	var b *types.Signed[*types.LaneProposal]
	for inner, ctrl := range state.inner.Lock() {
		q, ok := inner.blocks[lane]
		if !ok {
			return nil, ErrLaneClosed
		}
		n := q.next
		var parent types.BlockHeaderHash
		if q.first < q.next {
			parent = q.q[q.next-1].Msg().Block().Header().Hash()
		}
		b = types.Sign(key, types.NewLaneProposal(types.NewBlock(lane, n, parent, payload)))
		q.pushBack(b)
		ctrl.Updated()
	}
	return b, nil
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
		ds := newTestDataState(&data.Config{Registry: registry})
		sc.SpawnBgNamed("data.Run", func() error { return utils.IgnoreCancel(ds.Run(ctx)) })

		ep0 := registry.MustEpoch(0)
		qc0, blocks0 := data.TestCommitQC(rng, ep0, keys, utils.None[*types.CommitQC]())
		if err := ds.PushQC(ctx, qc0, blocks0); err != nil {
			return err
		}
		appHash0 := types.GenAppHash(rng)
		if err := ds.PushAppHash(ctx, qc0.QC().GlobalRange().Next-1, appHash0); err != nil {
			return err
		}
		if err := ds.PushAppQC(ctx, data.TestAppQC(keys, types.NewAppProposal(qc0.QC().Proposal(), appHash0))); err != nil {
			return err
		}
		if _, err := ds.Anchor().Wait(ctx, func(anchor utils.Option[data.Anchor]) bool {
			got, ok := anchor.Get()
			return ok && got.CommitQC.Index() == qc0.QC().Index()
		}); err != nil {
			return err
		}

		state, err := NewState(a, ds, utils.None[string]())
		if err != nil {
			return fmt.Errorf("NewState: %w", err)
		}
		sc.SpawnBgNamed("runEpochAdvance", func() error { return utils.IgnoreCancel(state.runEpochAdvance(ctx)) })

		lane0 := state.LocalLane().OrPanic("genesis")
		sub := state.SubscribeLaneProposals(lane0, 0)

		epLeave, err := registry.ActivateEpoch(
			0,
			map[types.PublicKey]uint64{b.Public(): 1},
			time.Time{}, registry.FirstBlock(),
		)
		if err != nil {
			return err
		}
		if epLeave.EpochIndex() != 2 {
			return fmt.Errorf("leave epoch = %d, want 2 (epoch 1 is genesis-seeded)", epLeave.EpochIndex())
		}
		// Data already holds an AppQC for epoch 0; NewState's prune stamped the
		// Anchor. Advance the seal cursor without admitting LastRoad tips — runEvict
		// is not running, and the rest of this test expects empty roads.
		seekRoads(state, epoch.FirstRoad(1))
		persistEpochSeal(state, ep0, keys)
		if _, err := state.Epoch().Wait(ctx, func(ep *types.Epoch) bool {
			return ep.EpochIndex() >= 1
		}); err != nil {
			return err
		}
		ep1 := registry.MustEpoch(1)
		for inner, ctrl := range state.inner.Lock() {
			inner.anchorEpoch = utils.Some(ep1)
			ctrl.Updated()
		}
		seekRoads(state, epoch.FirstRoad(2))
		persistEpochSeal(state, ep1, keys)
		if _, err := state.Epoch().Wait(ctx, func(ep *types.Epoch) bool {
			return ep.EpochIndex() >= epLeave.EpochIndex()
		}); err != nil {
			return err
		}

		for inner := range state.inner.Lock() {
			if _, ok := inner.blocks[lane0]; !ok {
				return fmt.Errorf("epoch advance must not drop closing-lane maps")
			}
		}

		// Construct a leave-epoch Anchor locally: data still holds only qc0, so a
		// FirstRoad(2) CommitQC cannot be pushed without filling the road gap.
		prev := tipLink(ep1, keys[0], epoch.LastRoad(1))
		qcLeave := types.BuildCommitQC(epLeave, []types.SecretKey{b}, utils.Some(prev), nil)
		anchor := data.Anchor{
			CommitQC: qcLeave,
			AppQC:    data.TestAppQC([]types.SecretKey{b}, types.NewAppProposal(qcLeave.Proposal(), types.AppHash{})),
			Epoch:    epLeave,
		}

		for inner, ctrl := range state.inner.Lock() {
			if inner.roads.first < inner.roads.next {
				return fmt.Errorf("roads should still be empty (seekRoads, no runEvict)")
			}
			if _, ok := inner.blocks[lane0]; !ok {
				return fmt.Errorf("closing lane maps should still be present before anchor prune")
			}
			n := inner.prune(anchor)
			if n != 1 {
				return fmt.Errorf("prune dropped %d lanes, want 1", n)
			}
			if _, ok := inner.blocks[lane0]; ok {
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
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		t.Logf("data.Run()")
		ds := newTestDataState(&data.Config{Registry: registry})
		s.SpawnBgNamed("data.Run", func() error { return utils.IgnoreCancel(ds.Run(ctx)) })

		t.Logf("Push FullCommitQC, blocks, AppHash, AppQC to data")
		qc, blocks := data.TestCommitQC(rng, epoch, keys, utils.None[*types.CommitQC]())
		if err := ds.PushQC(ctx, qc, blocks); err != nil {
			return err
		}
		appHash := types.GenAppHash(rng)
		if err := ds.PushAppHash(ctx, qc.QC().GlobalRange().Next-1, appHash); err != nil {
			return err
		}
		appQC := data.TestAppQC(keys, types.NewAppProposal(qc.QC().Proposal(), appHash))
		if err := ds.PushAppQC(ctx, appQC); err != nil {
			return err
		}

		t.Logf("wait for anchor to be updated")
		if _, err := ds.Anchor().Wait(ctx, func(anchor utils.Option[data.Anchor]) bool {
			a, ok := anchor.Get()
			return ok && a.CommitQC.Index() == qc.QC().Index()
		}); err != nil {
			return err
		}

		t.Logf("NewState() should load the anchor")
		state, err := NewState(keys[0], ds, utils.None[string]())
		if err != nil {
			return fmt.Errorf("NewState(): %w", err)
		}
		if err := utils.TestDiff(utils.Some(qc.QC()), state.LastCommitQC().Load()); err != nil {
			return err
		}
		t.Logf("avail.Run()")
		s.SpawnBgNamed("avail.Run", func() error { return utils.IgnoreCancel(state.Run(ctx)) })

		t.Logf("push next CommitQC to avail")
		qc, _ = data.TestCommitQC(rng, registry.MustEpoch(0), keys, utils.Some(qc.QC()))
		if err := state.PushCommitQC(ctx, qc.QC()); err != nil {
			return err
		}
		t.Logf("wait for this CommitQC to be persisted in avail")
		_, err = state.LastCommitQC().Wait(ctx, func(got utils.Option[*types.CommitQC]) bool {
			gotQC, ok := got.Get()
			return ok && gotQC.Index() == qc.QC().Index()
		})
		return err
	}))
}

func testState(t *testing.T, rng utils.Rng, stateDir utils.Option[string]) {
	t.Helper()
	ctx := t.Context()
	registry, keys := epoch.GenRegistry(rng, 3)
	committee := registry.MustEpoch(0).Committee()

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		ds := newTestDataState(&data.Config{Registry: registry})
		s.SpawnBgNamed("ds.Run()", func() error { return utils.IgnoreCancel(ds.Run(ctx)) })
		state, err := NewState(keys[0], ds, stateDir)
		if err != nil {
			return fmt.Errorf("NewState(): %w", err)
		}
		s.SpawnBgNamed("state.Run()", func() error { return utils.IgnoreCancel(state.Run(ctx)) })

		for i := range 3 {
			t.Logf("iteration %v", i)
			prev := state.LastCommitQC().Load()

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
			laneQCs, err := state.WaitForLaneQCs(ctx, registry.MustEpoch(0), prev)
			if err != nil {
				return fmt.Errorf("state.WaitForNewLaneQCs(): %w", err)
			}
			qc := types.BuildCommitQC(registry.MustEpoch(0), keys, prev, laneQCs)
			if err := state.PushCommitQC(ctx, qc); err != nil {
				return fmt.Errorf("state.PushCommitQC(): %w", err)
			}

			t.Logf("Check that a CommitQC was successfully reconstructed.")
			_, got, err := state.fullCommitQC(ctx, qc.Proposal().Index())
			if err != nil {
				return fmt.Errorf("state.fullCommitQC(): %w", err)
			}
			if err := utils.TestDiff(want, qcPayloadHashes(got)); err != nil {
				return fmt.Errorf("snapshot: %w", err)
			}

			t.Logf("Push app votes.")
			appHash := types.GenAppHash(rng)
			appProposal := types.NewAppProposal(qc.Proposal(), appHash)
			appGR := appProposal.GlobalRange()
			if err := ds.PushAppHash(ctx, appGR.Next-1, appHash); err != nil {
				return fmt.Errorf("ds.PushAppHash(): %w", err)
			}
			for _, vote := range makeAppVotes(keys, appProposal) {
				if err := state.PushAppVote(ctx, vote); err != nil {
					return fmt.Errorf("state.PushAppVote(): %w", err)
				}
			}

			t.Logf("Executed CommitQC should be eventually evicted")
			for inner, ctrl := range state.inner.Lock() {
				if err := ctrl.WaitUntil(ctx, func() bool { return inner.roads.first == appProposal.RoadIndex()+1 }); err != nil {
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
			gr := got.QC().GlobalRange()
			for i := gr.First; i < gr.Next; i++ {
				b, err := ds.Block(ctx, i)
				if err != nil {
					return fmt.Errorf("ds.Block(): %w", err)
				}
				if err := utils.TestDiff(b.Header(), got.Headers()[i-gr.First]); err != nil {
					return fmt.Errorf("snapshot: %w", err)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
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
	committee := registry.MustEpoch(0).Committee()
	dir := t.TempDir()

	// Phase 1: Run state with persistence through 2 iterations.
	var wantAppQCIdx types.RoadIndex
	var wantNextBlocks map[types.LaneID]types.BlockNumber
	// Hoisted so phase 1's WALs can be closed after its goroutines have stopped: the lane and
	// commitQC WALs hold an exclusive lock on their directories, which phase 2 reopens.
	var state1 *State

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		ds := newTestDataState(&data.Config{Registry: registry})
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

			laneQCs, err := state.WaitForLaneQCs(ctx, registry.MustEpoch(0), prev)
			if err != nil {
				return fmt.Errorf("WaitForLaneQCs: %w", err)
			}
			qc := types.BuildCommitQC(registry.MustEpoch(0), keys, prev, laneQCs)
			if err := state.PushCommitQC(ctx, qc); err != nil {
				return fmt.Errorf("PushCommitQC: %w", err)
			}

			appProposal := types.NewAppProposal(qc.Proposal(), types.GenAppHash(rng))
			for _, vote := range makeAppVotes(keys, appProposal) {
				if err := state.PushAppVote(ctx, vote); err != nil {
					return fmt.Errorf("PushAppVote: %w", err)
				}
			}
			if _, err := state.appQC(ctx, appProposal.RoadIndex()); err != nil {
				return fmt.Errorf("WaitForAppQC: %w", err)
			}
			prev = utils.Some(qc)
			wantAppQCIdx = appProposal.RoadIndex()
		}

		// Wait for commitQC persistence. markCommitQCsPersisted fires after
		// all commitQCs in the batch are on disk. Block goroutines may still
		// be in flight, but scope.Parallel in runPersist ensures they complete
		// before the next batch, so the data is durable by scope exit.
		if _, err := state.LastCommitQC().Wait(ctx, func(qc utils.Option[*types.CommitQC]) bool {
			return types.NextIndexOpt(qc) > wantAppQCIdx
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

	ds2 := newTestDataState(&data.Config{Registry: registry})
	state2, err := NewState(keys[0], ds2, utils.Some(dir))
	require.NoError(t, err)

	_, ok := state2.LastCommitQC().Load().Get()
	require.True(t, ok, "LastCommitQC should be set after restart")

	for lane := range committee.Lanes().All() {
		require.Equal(t, wantNextBlocks[lane], state2.NextBlock(lane),
			"NextBlock(%v) should match pre-restart value", lane)
	}
}

func TestPushBlockRejectsBadParentHash(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	ds := newTestDataState(&data.Config{Registry: registry})
	state := utils.OrPanic1(NewState(keys[0], ds, utils.Some(t.TempDir())))

	committee := registry.MustEpoch(0).Committee()
	lane := committee.Lane(keys[0].Public()).OrPanic("lane")
	// Produce a valid first block on our lane.
	_, err := state.ProduceLocalBlock(lane, state.NextBlock(lane), types.GenPayload(rng))
	require.NoError(t, err)

	// Create a second block with a fake parentHash.
	fakeBlock := types.NewBlock(lane, 1, types.GenBlockHeaderHash(rng), types.GenPayload(rng))
	fakeProp := types.Sign(keys[0], types.NewLaneProposal(fakeBlock))

	// Producer equivocation is logged but not returned as an error.
	require.NoError(t, state.PushBlock(ctx, fakeProp))
	// Queue did not advance — the bad block was dropped.
	require.Equal(t, types.BlockNumber(1), state.NextBlock(lane))
}

func TestPushBlockRejectsWrongSigner(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)

	ds := newTestDataState(&data.Config{Registry: registry})
	state := utils.OrPanic1(NewState(keys[0], ds, utils.Some(t.TempDir())))

	lane := registry.MustEpoch(0).Committee().Lane(keys[0].Public()).OrPanic("lane")
	// Create a block on keys[0]'s lane but sign it with keys[1].
	block := types.NewBlock(lane, 0, types.GenBlockHeaderHash(rng), types.GenPayload(rng))
	prop := types.Sign(keys[1], types.NewLaneProposal(block))

	err := state.PushBlock(ctx, prop)
	require.Error(t, err)
}

func TestNewStateWithPersistence(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	t.Run("empty dir loads fresh state", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(&data.Config{Registry: registry})

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		// Queues start at 0.
		require.Equal(t, types.RoadIndex(0), state.First())
	})

	t.Run("loads persisted blocks", func(t *testing.T) {
		dir := t.TempDir()
		ds := newTestDataState(&data.Config{Registry: registry})
		lane := registry.MustEpoch(0).Committee().Lane(keys[0].Public()).OrPanic("lane")

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
		ds := newTestDataState(&data.Config{Registry: registry})

		// Persist CommitQCs to disk.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)

		qcs := make([]*types.CommitQC, 3)
		prev := utils.None[*types.CommitQC]()
		for i := range qcs {
			qcs[i] = types.BuildCommitQC(registry.MustEpoch(0), keys, prev, nil)
			prev = utils.Some(qcs[i])
			require.NoError(t, cp.PruneAndPersist(0, []*types.CommitQC{qcs[i]}))
		}

		// Release the seeding persister's WAL locks before NewState opens the same directory.
		require.NoError(t, cp.Close())

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		// All 3 commitQCs should be loaded (no AppQC to skip past).
		require.Equal(t, types.RoadIndex(0), state.First())
		// LastCommitQC should be set to the last loaded one.
		require.NoError(t, utils.TestDiff(utils.Some(qcs[2]), state.LastCommitQC().Load()))
	})

	t.Run("non-contiguous commitQC files return error", func(t *testing.T) {
		dir := t.TempDir()

		// Build 6 sequential CommitQCs (indices 0-5).
		allQCs := make([]*types.CommitQC, 6)
		prev := utils.None[*types.CommitQC]()
		for i := range allQCs {
			allQCs[i] = types.BuildCommitQC(registry.MustEpoch(0), keys, prev, nil)
			prev = utils.Some(allQCs[i])
		}

		// Persist QCs 0, 1, 2 contiguously, then try to skip to 5.
		// Persist enforces strict sequential order, so the gap
		// is caught at write time rather than at load time.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		for i := range 3 {
			require.NoError(t, cp.PruneAndPersist(0, []*types.CommitQC{allQCs[i]}))
		}
		err = cp.PruneAndPersist(0, []*types.CommitQC{allQCs[5]})
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
		registry, err := epoch.NewRegistry(genesis, 0, time.Time{})
		require.NoError(t, err)
		ds := newTestDataState(&data.Config{Registry: registry})
		state, err := NewState(stay, ds, utils.None[string]())
		require.NoError(t, err)

		require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
			sc.SpawnBgNamed("runEpochAdvance", func() error {
				return utils.IgnoreCancel(state.runEpochAdvance(ctx))
			})

			ep0 := registry.MustEpoch(0)
			lane := ep0.Committee().Lane(stay.Public()).OrPanic("stay lane")
			header := types.NewBlock(lane, 0, types.BlockHeaderHash{}, &types.Payload{}).Header()
			leaverVote := types.Sign(leaver, types.NewLaneVote(header))

			epLeave, err := registry.ActivateEpoch(
				0,
				map[types.PublicKey]uint64{stay.Public(): 1, a.Public(): 1, b.Public(): 1},
				time.Time{}, registry.FirstBlock(),
			)
			if err != nil {
				return err
			}
			keys := []types.SecretKey{stay, leaver, a, b}
			if err := DriveAdvance(ctx, state, keys, epLeave.EpochIndex()); err != nil {
				return err
			}

			for inner := range state.inner.Lock() {
				if inner.applied().EpochIndex() != epLeave.EpochIndex() {
					return fmt.Errorf("applied epoch = %d, want %d", inner.applied().EpochIndex(), epLeave.EpochIndex())
				}
				ae, ok := inner.anchorEpoch.Get()
				if !ok {
					return fmt.Errorf("anchor epoch missing")
				}
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
	f := newSealFixture(t)
	_, err := f.registry.EpochAt(epoch.FirstRoad(f.m + 1))
	require.Error(t, err)

	seekRoads(f.state, epoch.FirstRoad(f.m))
	epPrev := f.registry.MustEpoch(f.m - 1)
	prev := tipLink(epPrev, f.keys[0], epoch.LastRoad(f.m-1))
	qc := types.BuildCommitQC(f.ep, f.keys, utils.Some(prev), nil)
	require.Equal(t, epoch.FirstRoad(f.m), qc.Proposal().Index())

	require.NoError(t, f.state.PushCommitQC(t.Context(), qc))
	require.Equal(t, epoch.FirstRoad(f.m)+1, nextRoad(f.state))
}

func TestPushCommitQC_StaleAfterAdvanceSoftDrops(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	ep1 := registry.MustEpoch(1)
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBgNamed("runEpochAdvance", func() error {
			return utils.IgnoreCancel(state.runEpochAdvance(ctx))
		})
		return DriveAdvance(ctx, state, keys, ep1.EpochIndex())
	}))

	ep0 := registry.MustEpoch(0)
	before := nextRoad(state)
	qc0 := types.BuildCommitQC(ep0, keys, utils.None[*types.CommitQC](), nil)
	require.Equal(t, types.EpochIndex(0), qc0.Proposal().EpochIndex())
	require.NoError(t, state.PushCommitQC(t.Context(), qc0))
	require.Equal(t, before, nextRoad(state))
}

type sealFixture struct {
	registry *epoch.Registry
	keys     []types.SecretKey
	state    *State
	ep       *types.Epoch
	m        types.EpochIndex
}

func newSealFixture(t *testing.T) *sealFixture {
	t.Helper()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	ds := newTestDataState(&data.Config{Registry: registry})
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)

	const m types.EpochIndex = 1
	ep := registry.MustEpoch(m)
	_, err = registry.EpochAt(epoch.FirstRoad(m + 1))
	require.Error(t, err, "epoch 2 must be absent for exec-leash tests")

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBgNamed("runEpochAdvance", func() error {
			return utils.IgnoreCancel(state.runEpochAdvance(ctx))
		})
		return DriveAdvance(ctx, state, keys, m)
	}))
	seekRoads(state, epoch.LastRoad(m))
	return &sealFixture{registry: registry, keys: keys, state: state, ep: ep, m: m}
}

func TestRunEpochAdvance_Leashes(t *testing.T) {
	type missing int
	const (
		none missing = iota
		registry
		appQC
	)
	for _, tc := range []struct {
		name       string
		missing    missing
		parkNextQC bool
	}{
		{name: "both met parks future QC until advance", missing: none, parkNextQC: true},
		{name: "waits for registry", missing: registry},
		{name: "waits for AppQC", missing: appQC},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				rng := utils.TestRng()
				f := newSealFixture(t)
				if tc.missing != registry {
					f.registry.AdvanceIfNeeded(epoch.LastRoad(f.m))
				}

				prev := tipLink(f.ep, f.keys[0], epoch.LastRoad(f.m)-1)
				qcLast := types.BuildCommitQC(f.ep, f.keys, utils.Some(prev), nil)
				require.Equal(t, epoch.LastRoad(f.m), qcLast.Proposal().Index())
				require.NoError(t, f.state.PushCommitQC(ctx, qcLast))
				f.state.markCommitQCsPersisted(qcLast)
				if tc.missing != appQC {
					setRoadAppQC(f.state, qcLast.Index(), data.TestAppQC(f.keys, types.NewAppProposal(qcLast.Proposal(), types.GenAppHash(rng))))
				}
				require.Equal(t, f.m, f.state.Epoch().Load().EpochIndex())

				var pushErr error
				if tc.parkNextQC {
					ep2 := f.registry.MustEpoch(f.m + 1)
					qcNext := types.BuildCommitQC(ep2, f.keys, utils.Some(qcLast), nil)
					require.Equal(t, epoch.FirstRoad(f.m+1), qcNext.Proposal().Index())
					go func() { pushErr = f.state.PushCommitQC(ctx, qcNext) }()
					synctest.Wait()
					require.Equal(t, epoch.LastRoad(f.m)+1, nextRoad(f.state), "future QC must stay parked")
				}

				var runErr error
				go func() { runErr = f.state.runEpochAdvance(ctx) }()
				if tc.missing != none {
					synctest.Wait()
					require.Equal(t, f.m, f.state.Epoch().Load().EpochIndex())
					switch tc.missing {
					case registry:
						f.registry.AdvanceIfNeeded(epoch.LastRoad(f.m))
					case appQC:
						setRoadAppQC(f.state, qcLast.Index(), data.TestAppQC(f.keys, types.NewAppProposal(qcLast.Proposal(), types.GenAppHash(rng))))
					}
				}

				ep, err := f.state.Epoch().Wait(t.Context(), func(ep *types.Epoch) bool {
					return ep.EpochIndex() >= f.m+1
				})
				require.NoError(t, err)
				require.Equal(t, f.m+1, ep.EpochIndex())
				require.Equal(t, f.m+1, f.state.Epoch().Load().EpochIndex())
				if tc.parkNextQC {
					synctest.Wait()
					require.NoError(t, pushErr)
					require.Equal(t, epoch.FirstRoad(f.m+1)+1, nextRoad(f.state))
				}

				cancel()
				synctest.Wait()
				require.ErrorIs(t, runErr, context.Canceled)
			})
		})
	}
}

// A durable-tip catch-up refreshes ConsensusSpec at the persist write site,
// even while epoch advance is parked on WaitForEpoch.
func TestMarkCommitQCsPersisted_RefreshesSpecWhileEpochAdvanceWaitsForRegistry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		rng := utils.TestRng()
		f := newSealFixture(t)

		last := epoch.LastRoad(f.m)
		seekRoads(f.state, last-2)
		prev := tipLink(f.ep, f.keys[0], last-3)
		qcA := types.BuildCommitQC(f.ep, f.keys, utils.Some(prev), nil)
		qcB := types.BuildCommitQC(f.ep, f.keys, utils.Some(qcA), nil)
		qcC := types.BuildCommitQC(f.ep, f.keys, utils.Some(qcB), nil)
		require.Equal(t, last-2, qcA.Index())
		require.Equal(t, last-1, qcB.Index())
		require.Equal(t, last, qcC.Index())
		require.NoError(t, f.state.PushCommitQC(ctx, qcA))
		require.NoError(t, f.state.PushCommitQC(ctx, qcB))
		require.NoError(t, f.state.PushCommitQC(ctx, qcC))
		setRoadAppQC(f.state, qcC.Index(), data.TestAppQC(f.keys, types.NewAppProposal(qcC.Proposal(), types.GenAppHash(rng))))
		f.state.markCommitQCsPersisted(qcA)

		spec := f.state.SubscribeConsensusSpec()
		var advanceErr error
		go func() { advanceErr = f.state.runEpochAdvance(ctx) }()
		synctest.Wait()
		require.Equal(t, f.m, f.state.Epoch().Load().EpochIndex(), "parked on WaitForEpoch(M+1)")
		got, ok := spec.Load().CommitQC.Get()
		require.True(t, ok)
		require.Equal(t, qcA.Index(), got.Index())

		f.state.markCommitQCsPersisted(qcB)
		got, ok = spec.Load().CommitQC.Get()
		require.True(t, ok)
		require.Equal(t, qcB.Index(), got.Index())
		require.Equal(t, f.m, f.state.Epoch().Load().EpochIndex(), "still waiting on registry")

		cancel()
		synctest.Wait()
		require.ErrorIs(t, advanceErr, context.Canceled)
	})
}
