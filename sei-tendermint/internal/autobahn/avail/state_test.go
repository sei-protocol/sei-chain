package avail

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/stretchr/testify/require"
)

var (
	noBlockCB    = utils.None[func(*types.Signed[*types.LaneProposal])]()
	noCommitQCCB = utils.None[func(*types.CommitQC)]()
)

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

func leaderKey(committee *types.Committee, keys []types.SecretKey, view types.View) types.SecretKey {
	leader := committee.Leader(view)
	for _, k := range keys {
		if k.Public() == leader {
			return k
		}
	}
	panic("leader not in keys")
}

func makeCommitQC(
	committee *types.Committee,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
	laneQCs map[types.LaneID]*types.LaneQC,
	appQC utils.Option[*types.AppQC],
) *types.CommitQC {
	vs := types.ViewSpec{CommitQC: prev}
	fullProposal := utils.OrPanic1(types.NewProposal(
		leaderKey(committee, keys, vs.View()),
		committee,
		vs,
		time.Now(),
		laneQCs,
		appQC,
	))
	vote := types.NewCommitVote(fullProposal.Proposal().Msg())
	var votes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewCommitQC(votes)
}

func qcPayloadHashes(qc *types.FullCommitQC) byLane[types.PayloadHash] {
	x := byLane[types.PayloadHash]{}
	for _, h := range qc.Headers() {
		x[h.Lane()] = append(x[h.Lane()], h.PayloadHash())
	}
	return x
}

func TestState(t *testing.T) {
	testState(t, utils.None[string]())
}

// TestStateWithPersistence runs the same flow as TestState but with disk
// persistence enabled. The persist goroutine and prune (triggered by AppQC)
// run concurrently, exercising the cursor-clamp logic that prevents reading
// pruned map entries.
func TestStateWithPersistence(t *testing.T) {
	for range 5 {
		testState(t, utils.Some(t.TempDir()))
	}
}

func testState(t *testing.T, stateDir utils.Option[string]) {
	t.Helper()
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		ds := data.NewState(&data.Config{
			Committee: committee,
		}, utils.None[data.BlockStore]())
		s.SpawnBgNamed("data.State.Run()", func() error {
			return utils.IgnoreCancel(ds.Run(ctx))
		})
		state, err := NewState(keys[0], ds, stateDir)
		require.NoError(t, err)
		s.SpawnBgNamed("da.State.Run()", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		for i := range 3 {
			t.Logf("iteration %v", i)
			prev := state.LastCommitQC().Load()

			t.Logf("Push some blocks.")
			want := byLane[types.PayloadHash]{}
			for range 10 {
				key := keys[rng.Intn(len(keys))]
				lane := key.Public()
				p := types.GenPayload(rng)
				want[lane] = append(want[lane], p.Hash())
				b, err := state.produceBlock(ctx, key, p)
				if err != nil {
					return fmt.Errorf("state.ProduceBlock(): %w", err)
				}
				if err := utils.TestDiff(b.Msg().Block().Payload(), p); err != nil {
					return fmt.Errorf("snapshot: %w", err)
				}
			}

			t.Logf("Push votes for all the blocks.")
			for _, lane := range committee.Lanes().All() {
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
			laneQCs, err := state.WaitForLaneQCs(ctx, prev)
			if err != nil {
				return fmt.Errorf("state.WaitForNewLaneQCs(): %w", err)
			}
			qc := makeCommitQC(committee, keys, prev, laneQCs, state.LastAppQC())
			if err := state.PushCommitQC(ctx, qc); err != nil {
				return fmt.Errorf("state.PushCommitQC(): %w", err)
			}

			t.Logf("Push app votes.")
			appProposal := types.NewAppProposal(qc.GlobalRange(committee).Next-1, qc.Proposal().Index(), types.GenAppHash(rng))
			for _, vote := range makeAppVotes(keys, appProposal) {
				if err := state.PushAppVote(ctx, vote); err != nil {
					return fmt.Errorf("state.PushAppVote(): %w", err)
				}
			}

			t.Logf("Previous one should be pruned because of appQC.")
			if _, _, err := state.WaitForAppQC(ctx, appProposal.RoadIndex()); err != nil {
				return fmt.Errorf("state.WaitForAppQC(): %w", err)
			}
			if prev, ok := prev.Get(); ok {
				if _, err := state.CommitQC(ctx, prev.Proposal().Index()); !errors.Is(err, data.ErrPruned) {
					return fmt.Errorf("state.CommitQC(): %w, want %v", err, data.ErrPruned.Error())
				}
			}

			t.Logf("Check that the executed local blocks have been pruned")
			for _, lane := range committee.Lanes().All() {
				if lr := types.LaneRangeOpt(prev, lane); lr.Next() > 0 {
					if _, err := state.Block(ctx, lane, lr.Next()-1); !errors.Is(err, data.ErrPruned) {
						return fmt.Errorf("state.Block(): %w, want %v", err, data.ErrPruned.Error())
					}
				}
			}

			t.Logf("Check that a CommitQC was successfully reconstructed.")
			got, err := state.fullCommitQC(ctx, qc.Proposal().Index())
			if err != nil {
				return fmt.Errorf("state.fullCommitQC(): %w", err)
			}
			if err := utils.TestDiff(want, qcPayloadHashes(got)); err != nil {
				return fmt.Errorf("snapshot: %w", err)
			}

			t.Logf("Check that the blocks were successfully pushed to data state.")
			gr := got.QC().GlobalRange(committee)
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
// After iteration 0's AppQC prunes old data, iteration 1 writes new blocks
// and commitQCs at higher indices. If WAL truncation hasn't cleaned up the
// stale entries by shutdown, restart exercises the gap-filtering path in
// loadPersistedState (stale entries below the prune anchor are discarded).
func TestStateRestartFromPersisted(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	dir := t.TempDir()

	// Phase 1: Run state with persistence through 2 iterations.
	var wantAppQCIdx types.RoadIndex
	var wantNextBlocks map[types.LaneID]types.BlockNumber

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())
		s.SpawnBgNamed("data.Run", func() error {
			return utils.IgnoreCancel(ds.Run(ctx))
		})
		state, err := NewState(keys[0], ds, utils.Some(dir))
		if err != nil {
			return err
		}
		s.SpawnBgNamed("avail.Run", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		for i := range 2 {
			t.Logf("iteration %d", i)
			prev := state.LastCommitQC().Load()

			for range 5 {
				key := keys[rng.Intn(len(keys))]
				if _, err := state.produceBlock(ctx, key, types.GenPayload(rng)); err != nil {
					return fmt.Errorf("produceBlock: %w", err)
				}
			}

			for _, lane := range committee.Lanes().All() {
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

			laneQCs, err := state.WaitForLaneQCs(ctx, prev)
			if err != nil {
				return fmt.Errorf("WaitForLaneQCs: %w", err)
			}
			qc := makeCommitQC(committee, keys, prev, laneQCs, state.LastAppQC())
			if err := state.PushCommitQC(ctx, qc); err != nil {
				return fmt.Errorf("PushCommitQC: %w", err)
			}

			appProposal := types.NewAppProposal(qc.GlobalRange(committee).Next-1, qc.Proposal().Index(), types.GenAppHash(rng))
			for _, vote := range makeAppVotes(keys, appProposal) {
				if err := state.PushAppVote(ctx, vote); err != nil {
					return fmt.Errorf("PushAppVote: %w", err)
				}
			}
			if _, _, err := state.WaitForAppQC(ctx, appProposal.RoadIndex()); err != nil {
				return fmt.Errorf("WaitForAppQC: %w", err)
			}
			wantAppQCIdx = appProposal.RoadIndex()
		}

		// Wait for commitQC persistence. markCommitQCsPersisted fires after
		// all commitQCs in the batch are on disk. Block goroutines may still
		// be in flight, but scope.Parallel in runPersist ensures they complete
		// before the next batch, so the data is durable by scope exit.
		if err := state.waitForCommitQC(ctx, wantAppQCIdx); err != nil {
			return fmt.Errorf("waitForCommitQC: %w", err)
		}

		wantNextBlocks = make(map[types.LaneID]types.BlockNumber, committee.Lanes().Len())
		for _, lane := range committee.Lanes().All() {
			wantNextBlocks[lane] = state.NextBlock(lane)
		}
		return nil
	}))

	// Phase 2: Restart from the same directory.
	ds2 := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())
	state2, err := NewState(keys[0], ds2, utils.Some(dir))
	require.NoError(t, err)

	got, ok := state2.LastAppQC().Get()
	require.True(t, ok, "AppQC should be restored after restart")
	require.Equal(t, wantAppQCIdx, got.Proposal().RoadIndex())

	require.GreaterOrEqual(t, state2.FirstCommitQC(), wantAppQCIdx)

	_, ok = state2.LastCommitQC().Load().Get()
	require.True(t, ok, "LastCommitQC should be set after restart")

	for _, lane := range committee.Lanes().All() {
		require.Equal(t, wantNextBlocks[lane], state2.NextBlock(lane),
			"NextBlock(%v) should match pre-restart value", lane)
	}
}

func TestStateMismatchedQCs(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	initialBlock := committee.FirstBlock()

	ds := data.NewState(&data.Config{
		Committee: committee,
	}, utils.None[data.BlockStore]())
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)
	ctx := t.Context()

	// Helper to create a CommitQC for a specific index
	makeQC := func(prev utils.Option[*types.CommitQC], laneQCs map[types.LaneID]*types.LaneQC) *types.CommitQC {
		vs := types.ViewSpec{CommitQC: prev}
		fullProposal := utils.OrPanic1(types.NewProposal(
			leaderKey(committee, keys, vs.View()),
			committee,
			vs,
			time.Now(),
			laneQCs,
			utils.None[*types.AppQC](),
		))
		vote := types.NewCommitVote(fullProposal.Proposal().Msg())
		var votes []*types.Signed[*types.CommitVote]
		for _, k := range keys {
			votes = append(votes, types.Sign(k, vote))
		}
		return types.NewCommitQC(votes)
	}

	// 1. Produce a block so we have a non-empty range
	lane := keys[0].Public()
	p := types.GenPayload(rng)
	b, err := state.ProduceBlock(ctx, p)
	require.NoError(t, err)

	// 2. Form a LaneQC for it
	laneVotes := makeLaneVotes(keys, b.Msg().Block().Header())
	laneQC := types.NewLaneQC(laneVotes[:2]) // f+1 = 2 for 4 nodes

	// 3. Create CommitQC for index 0 (finalizes block 0)
	qc0 := makeQC(utils.None[*types.CommitQC](), map[types.LaneID]*types.LaneQC{lane: laneQC})
	require.Equal(t, initialBlock, qc0.GlobalRange(committee).First)
	require.Equal(t, initialBlock+1, qc0.GlobalRange(committee).Next)

	t.Run("PushAppQC mismatch", func(t *testing.T) {
		require := require.New(t)
		// AppQC for index 1, but paired with CommitQC for index 0
		appProposal1 := types.NewAppProposal(initialBlock, 1, types.GenAppHash(rng))
		appQC1 := types.NewAppQC(makeAppVotes(keys, appProposal1))

		err := state.PushAppQC(appQC1, qc0)
		require.Error(err)
	})
}

func TestPushBlockRejectsBadParentHash(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)

	ds := data.NewState(&data.Config{
		Committee: committee,
	}, utils.None[data.BlockStore]())
	state := utils.OrPanic1(NewState(keys[0], ds, utils.None[string]()))

	// Produce a valid first block on our lane.
	_, err := state.ProduceBlock(ctx, types.GenPayload(rng))
	require.NoError(t, err)

	// Create a second block with a fake parentHash.
	lane := keys[0].Public()
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
	committee, keys := types.GenCommittee(rng, 3)

	ds := data.NewState(&data.Config{
		Committee: committee,
	}, utils.None[data.BlockStore]())
	state := utils.OrPanic1(NewState(keys[0], ds, utils.None[string]()))

	// Create a block on keys[0]'s lane but sign it with keys[1].
	lane := keys[0].Public()
	block := types.NewBlock(lane, 0, types.GenBlockHeaderHash(rng), types.GenPayload(rng))
	prop := types.Sign(keys[1], types.NewLaneProposal(block))

	err := state.PushBlock(ctx, prop)
	require.Error(t, err)
}

func TestNewStateWithPersistence(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	initialBlock := committee.FirstBlock()

	t.Run("empty dir loads fresh state", func(t *testing.T) {
		dir := t.TempDir()
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		// No persisted AppQC → None.
		require.False(t, state.LastAppQC().IsPresent())
		// Queues start at 0.
		require.Equal(t, types.RoadIndex(0), state.FirstCommitQC())
	})

	t.Run("loads persisted AppQC", func(t *testing.T) {
		dir := t.TempDir()
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())

		roadIdx := types.RoadIndex(7)
		globalNum := types.GlobalBlockNumber(50)
		appProposal := types.NewAppProposal(globalNum, roadIdx, types.GenAppHash(rng))
		appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

		// Persist commitQCs 0-7 so the matching one at roadIdx exists.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		prev := utils.None[*types.CommitQC]()
		var pruneQC *types.CommitQC
		for i := types.RoadIndex(0); i <= roadIdx; i++ {
			qc := makeCommitQC(committee, keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qc)
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{qc}, noCommitQCCB))
			pruneQC = qc
		}

		// Persist prune anchor (AppQC + CommitQC pair).
		prunePers, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		require.NoError(t, prunePers.Persist(&pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(appQC),
			CommitQc: types.CommitQCConv.Encode(pruneQC),
		}))

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		aq := state.LastAppQC()
		got, ok := aq.Get()
		require.True(t, ok)
		require.Equal(t, roadIdx, got.Proposal().RoadIndex())
		require.Equal(t, globalNum, got.Proposal().GlobalNumber())

		require.Equal(t, roadIdx, state.FirstCommitQC())
	})

	t.Run("loads persisted blocks", func(t *testing.T) {
		dir := t.TempDir()
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())
		lane := keys[0].Public()

		// Persist blocks using BlockPersister.
		bp, _, err := persist.NewBlockPersister(utils.Some(dir))
		require.NoError(t, err)

		var parent types.BlockHeaderHash
		for n := range types.BlockNumber(3) {
			block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
			signed := types.Sign(keys[0], types.NewLaneProposal(block))
			parent = block.Header().Hash()
			require.NoError(t, bp.MaybePruneAndPersistLane(lane, utils.None[*types.CommitQC](), []*types.Signed[*types.LaneProposal]{signed}, noBlockCB))
		}

		// Now construct state — it should load the blocks.
		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		require.Equal(t, types.BlockNumber(3), state.NextBlock(lane))
	})

	t.Run("loads persisted AppQC and blocks together", func(t *testing.T) {
		dir := t.TempDir()
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())
		lane := keys[0].Public()

		roadIdx := types.RoadIndex(2)
		globalNum := types.GlobalBlockNumber(5)
		appProposal := types.NewAppProposal(globalNum, roadIdx, types.GenAppHash(rng))
		appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

		// Persist commitQCs 0-2 so the matching one at roadIdx exists.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		prev := utils.None[*types.CommitQC]()
		var pruneQC *types.CommitQC
		for range roadIdx + 1 {
			qc := makeCommitQC(committee, keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qc)
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{qc}, noCommitQCCB))
			pruneQC = qc
		}

		// Persist prune anchor (AppQC + CommitQC pair).
		prunePers, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		require.NoError(t, prunePers.Persist(&pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(appQC),
			CommitQc: types.CommitQCConv.Encode(pruneQC),
		}))

		// Persist blocks starting at 0 (nil laneQCs → lr.First()=0 after prune).
		bp, _, err := persist.NewBlockPersister(utils.Some(dir))
		require.NoError(t, err)

		var parent types.BlockHeaderHash
		for n := range types.BlockNumber(3) {
			block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
			signed := types.Sign(keys[0], types.NewLaneProposal(block))
			parent = block.Header().Hash()
			require.NoError(t, bp.MaybePruneAndPersistLane(lane, utils.None[*types.CommitQC](), []*types.Signed[*types.LaneProposal]{signed}, noBlockCB))
		}

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		got, ok := state.LastAppQC().Get()
		require.True(t, ok)
		require.Equal(t, roadIdx, got.Proposal().RoadIndex())

		require.Equal(t, types.BlockNumber(3), state.NextBlock(lane))
		require.Equal(t, roadIdx, state.FirstCommitQC())
	})

	t.Run("loads persisted commitQCs", func(t *testing.T) {
		dir := t.TempDir()
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())

		// Persist CommitQCs to disk.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)

		qcs := make([]*types.CommitQC, 3)
		prev := utils.None[*types.CommitQC]()
		for i := range qcs {
			qcs[i] = makeCommitQC(committee, keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qcs[i])
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{qcs[i]}, noCommitQCCB))
		}

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		// All 3 commitQCs should be loaded (no AppQC to skip past).
		require.Equal(t, types.RoadIndex(0), state.FirstCommitQC())
		// LastCommitQC should be set to the last loaded one.
		require.NoError(t, utils.TestDiff(utils.Some(qcs[2]), state.LastCommitQC().Load()))
	})

	t.Run("loads persisted commitQCs with AppQC", func(t *testing.T) {
		dir := t.TempDir()
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())

		// Persist AppQC at road index 1.
		roadIdx := types.RoadIndex(1)
		globalNum := types.GlobalBlockNumber(5)
		appProposal := types.NewAppProposal(globalNum, roadIdx, types.GenAppHash(rng))
		appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

		// Persist CommitQCs 0-4.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)

		qcs := make([]*types.CommitQC, 5)
		prev := utils.None[*types.CommitQC]()
		for i := range qcs {
			qcs[i] = makeCommitQC(committee, keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qcs[i])
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{qcs[i]}, noCommitQCCB))
		}

		// Persist prune anchor (AppQC + CommitQC pair at roadIdx).
		prunePers, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		require.NoError(t, prunePers.Persist(&pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(appQC),
			CommitQc: types.CommitQCConv.Encode(qcs[roadIdx]),
		}))

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		// inner.prune(appQC@1, commitQC@1) sets commitQCs.first = 1.
		require.Equal(t, types.RoadIndex(1), state.FirstCommitQC())
		require.NoError(t, utils.TestDiff(utils.Some(qcs[4]), state.LastCommitQC().Load()))
	})

	t.Run("non-contiguous commitQC files return error", func(t *testing.T) {
		dir := t.TempDir()

		// Build 6 sequential CommitQCs (indices 0-5).
		allQCs := make([]*types.CommitQC, 6)
		prev := utils.None[*types.CommitQC]()
		for i := range allQCs {
			allQCs[i] = makeCommitQC(committee, keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(allQCs[i])
		}

		// Persist prune anchor (AppQC + CommitQC pair at road index 0).
		appProposal := types.NewAppProposal(initialBlock, 0, types.GenAppHash(rng))
		appQC := types.NewAppQC(makeAppVotes(keys, appProposal))
		prunePers, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		require.NoError(t, prunePers.Persist(&pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(appQC),
			CommitQc: types.CommitQCConv.Encode(allQCs[0]),
		}))

		// Persist QCs 0, 1, 2 contiguously, then try to skip to 5.
		// MaybePruneAndPersist enforces strict sequential order, so the gap
		// is caught at write time rather than at load time.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		for i := range 3 {
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{allQCs[i]}, noCommitQCCB))
		}
		err = cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{allQCs[5]}, noCommitQCCB)
		require.Error(t, err)
		require.Contains(t, err.Error(), "out of sequence")
		require.NoError(t, cp.Close())
	})

	t.Run("anchor past all persisted commitQCs truncates WAL", func(t *testing.T) {
		dir := t.TempDir()
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())

		// Build a chain of 10 CommitQCs (indices 0-9).
		qcs := make([]*types.CommitQC, 10)
		prev := utils.None[*types.CommitQC]()
		for i := range qcs {
			qcs[i] = makeCommitQC(committee, keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qcs[i])
		}

		// Persist only indices 0-4 to the CommitQC WAL.
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		for i := range 5 {
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{qcs[i]}, noCommitQCCB))
		}
		require.NoError(t, cp.Close())

		// Persist a prune anchor at index 9 — well past the persisted range.
		appProposal := types.NewAppProposal(50, 9, types.GenAppHash(rng))
		appQC := types.NewAppQC(makeAppVotes(keys, appProposal))
		prunePers, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		require.NoError(t, prunePers.Persist(&pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(appQC),
			CommitQc: types.CommitQCConv.Encode(qcs[9]),
		}))

		// NewState should succeed: MaybePruneAndPersist truncates the stale WAL
		// and internally re-persists the anchor's CommitQC for crash recovery.
		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		require.Equal(t, types.RoadIndex(9), state.FirstCommitQC())
		require.NoError(t, utils.TestDiff(utils.Some(qcs[9]), state.LastCommitQC().Load()))

		got, ok := state.LastAppQC().Get()
		require.True(t, ok)
		require.Equal(t, types.RoadIndex(9), got.Proposal().RoadIndex())
	})

	t.Run("anchor past all persisted blocks truncates lane WAL", func(t *testing.T) {
		dir := t.TempDir()
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())
		lane := keys[0].Public()

		// Persist commitQCs 0-9 and blocks 0-2 for one lane.
		qcs := make([]*types.CommitQC, 10)
		prev := utils.None[*types.CommitQC]()
		cp, _, err := persist.NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		for i := range qcs {
			qcs[i] = makeCommitQC(committee, keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qcs[i])
			require.NoError(t, cp.MaybePruneAndPersist(utils.None[*types.CommitQC](), []*types.CommitQC{qcs[i]}, noCommitQCCB))
		}
		require.NoError(t, cp.Close())

		bp, _, err := persist.NewBlockPersister(utils.Some(dir))
		require.NoError(t, err)
		var parent types.BlockHeaderHash
		for n := range types.BlockNumber(3) {
			block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
			signed := types.Sign(keys[0], types.NewLaneProposal(block))
			parent = block.Header().Hash()
			require.NoError(t, bp.MaybePruneAndPersistLane(lane, utils.None[*types.CommitQC](), []*types.Signed[*types.LaneProposal]{signed}, noBlockCB))
		}

		// Persist a prune anchor at index 9 with a laneRange that starts past
		// all persisted blocks — MaybePruneAndPersistLane will TruncateAll the block WAL.
		appProposal := types.NewAppProposal(50, 9, types.GenAppHash(rng))
		appQC := types.NewAppQC(makeAppVotes(keys, appProposal))
		prunePers, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		require.NoError(t, prunePers.Persist(&pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(appQC),
			CommitQc: types.CommitQCConv.Encode(qcs[9]),
		}))

		// NewState should succeed: block WAL gets truncated, lane starts clean.
		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		require.Equal(t, types.RoadIndex(9), state.FirstCommitQC())
		got, ok := state.LastAppQC().Get()
		require.True(t, ok)
		require.Equal(t, types.RoadIndex(9), got.Proposal().RoadIndex())
	})

	t.Run("corrupt AppQC data returns error", func(t *testing.T) {
		dir := t.TempDir()
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())

		// Create a throwaway persister to discover the A/B filenames,
		// then corrupt them so NewState fails on load.
		_, _, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](utils.Some(dir), innerFile)
		require.NoError(t, err)
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			require.NoError(t, os.WriteFile(filepath.Join(dir, e.Name()), []byte("corrupt"), 0600))
		}

		_, err = NewState(keys[0], ds, utils.Some(dir))
		require.Error(t, err)
	})
}
