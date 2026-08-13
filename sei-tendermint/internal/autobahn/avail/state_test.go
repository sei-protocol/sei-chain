package avail

import (
	"context"
	"errors"
	"fmt"
	"testing"
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
	lane := state.data.Registry().LatestEpoch().Committee().Lane(key.Public()).OrPanic("lane")
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

// Test checking that State can correctly start collecting CommitQCs starting from arbitrary anchor.
func TestCollectPersistBatch_EmptyRoadsDropsClosedLane(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	a, b := keys[0], keys[1]

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
		ds := newTestDataState(&data.Config{Registry: registry})
		sc.SpawnBgNamed("data.Run", func() error { return utils.IgnoreCancel(ds.Run(ctx)) })

		ep0 := registry.LatestEpoch()
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
		lane0 := state.LocalLane().OrPanic("genesis")
		sub := state.SubscribeLaneProposals(lane0, 0)

		ep1, err := registry.ActivateEpoch(
			map[types.PublicKey]uint64{b.Public(): 1},
			types.OpenRoadRange(), time.Time{}, registry.FirstBlock(),
		)
		if err != nil {
			return err
		}
		state.ApplyEpoch(ep1)

		qc1, blocks1 := data.TestCommitQC(rng, ep1, []types.SecretKey{b}, utils.Some(qc0.QC()))
		if err := ds.PushQC(ctx, qc1, blocks1); err != nil {
			return err
		}
		appHash1 := types.GenAppHash(rng)
		if err := ds.PushAppHash(ctx, qc1.QC().GlobalRange().Next-1, appHash1); err != nil {
			return err
		}
		if err := ds.PushAppQC(ctx, data.TestAppQC([]types.SecretKey{b}, types.NewAppProposal(qc1.QC().Proposal(), appHash1))); err != nil {
			return err
		}
		if _, err := ds.Anchor().Wait(ctx, func(anchor utils.Option[data.Anchor]) bool {
			got, ok := anchor.Get()
			return ok && got.CommitQC.Index() == qc1.QC().Index()
		}); err != nil {
			return err
		}

		for inner := range state.inner.Lock() {
			if inner.roads.first < inner.roads.next {
				return fmt.Errorf("roads should be empty for the anchor-fallback path")
			}
			if _, ok := inner.blocks[lane0]; !ok {
				return fmt.Errorf("closing lane maps should still be present before collect")
			}
			if !hasClosedLane(inner, ds) {
				return fmt.Errorf("hasClosedLane: empty roads + epoch-1 anchor should see lane0 closed")
			}
		}

		if _, err := state.collectPersistBatch(ctx); err != nil {
			return fmt.Errorf("collectPersistBatch: %w", err)
		}
		for inner := range state.inner.Lock() {
			if _, ok := inner.blocks[lane0]; ok {
				return fmt.Errorf("collectPersistBatch should have dropped closed lane0")
			}
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
	epoch := registry.LatestEpoch()
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
		qc, _ = data.TestCommitQC(rng, registry.LatestEpoch(), keys, utils.Some(qc.QC()))
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
	committee := registry.LatestEpoch().Committee()

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
			laneQCs, err := state.WaitForLaneQCs(ctx, registry.LatestEpoch(), prev)
			if err != nil {
				return fmt.Errorf("state.WaitForNewLaneQCs(): %w", err)
			}
			qc := types.BuildCommitQC(registry.LatestEpoch(), keys, prev, laneQCs)
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
	committee := registry.LatestEpoch().Committee()
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

			laneQCs, err := state.WaitForLaneQCs(ctx, registry.LatestEpoch(), prev)
			if err != nil {
				return fmt.Errorf("WaitForLaneQCs: %w", err)
			}
			qc := types.BuildCommitQC(registry.LatestEpoch(), keys, prev, laneQCs)
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

	committee := registry.LatestEpoch().Committee()
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

	lane := registry.LatestEpoch().Committee().Lane(keys[0].Public()).OrPanic("lane")
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
		lane := registry.LatestEpoch().Committee().Lane(keys[0].Public()).OrPanic("lane")

		// Persist blocks using BlockPersister.
		bp, _, err := persist.NewBlockPersister(utils.Some(dir))
		require.NoError(t, err)

		var parent types.BlockHeaderHash
		for n := range types.BlockNumber(3) {
			block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
			signed := types.Sign(keys[0], types.NewLaneProposal(block))
			parent = block.Header().Hash()
			require.NoError(t, bp.MaybePruneAndPersistLane(
				lane,
				true,
				utils.None[types.BlockNumber](),
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
			qcs[i] = types.BuildCommitQC(registry.LatestEpoch(), keys, prev, nil)
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
			allQCs[i] = types.BuildCommitQC(registry.LatestEpoch(), keys, prev, nil)
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
