package avail

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/stretchr/testify/require"
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
	rng utils.Rng,
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
			qc := makeCommitQC(rng, committee, keys, prev, laneQCs, state.LastAppQC())
			if err := state.PushCommitQC(ctx, qc); err != nil {
				return fmt.Errorf("state.PushCommitQC(): %w", err)
			}

			t.Logf("Push app votes.")
			appProposal := types.NewAppProposal(qc.GlobalRange().Next-1, qc.Proposal().Index(), types.GenAppHash(rng))
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

func TestStateMismatchedQCs(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)

	ds := data.NewState(&data.Config{
		Committee: committee,
	}, utils.None[data.BlockStore]())
	state, err := NewState(keys[0], ds, utils.None[string]())
	require.NoError(t, err)
	ctx := context.Background()

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
	require.Equal(t, types.GlobalBlockNumber(0), qc0.GlobalRange().First)
	require.Equal(t, types.GlobalBlockNumber(1), qc0.GlobalRange().Next)

	t.Run("PushAppQC mismatch", func(t *testing.T) {
		require := require.New(t)
		// AppQC for index 1, but paired with CommitQC for index 0
		appProposal1 := types.NewAppProposal(0, 1, types.GenAppHash(rng))
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

		persister, _, err := persist.NewPersister[*apb.AppQC](dir, innerFile)
		require.NoError(t, err)
		require.NoError(t, persister.Persist(types.AppQCConv.Encode(appQC)))

		// Persist commitQCs 0-7 so the matching one at roadIdx exists.
		cp, _, err := persist.NewCommitQCPersister(dir)
		require.NoError(t, err)
		prev := utils.None[*types.CommitQC]()
		for i := types.RoadIndex(0); i <= roadIdx; i++ {
			qc := makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qc)
			require.NoError(t, cp.PersistCommitQC(qc))
		}

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
		bp, _, err := persist.NewBlockPersister(dir)
		require.NoError(t, err)

		var parent types.BlockHeaderHash
		for n := types.BlockNumber(0); n < 3; n++ {
			block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
			signed := types.Sign(keys[0], types.NewLaneProposal(block))
			parent = block.Header().Hash()
			require.NoError(t, bp.PersistBlock(signed))
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

		persister, _, err := persist.NewPersister[*apb.AppQC](dir, innerFile)
		require.NoError(t, err)
		require.NoError(t, persister.Persist(types.AppQCConv.Encode(appQC)))

		// Persist commitQCs 0-2 so the matching one at roadIdx exists.
		cp, _, err := persist.NewCommitQCPersister(dir)
		require.NoError(t, err)
		prev := utils.None[*types.CommitQC]()
		for i := types.RoadIndex(0); i <= roadIdx; i++ {
			qc := makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qc)
			require.NoError(t, cp.PersistCommitQC(qc))
		}

		// Persist blocks.
		bp, _, err := persist.NewBlockPersister(dir)
		require.NoError(t, err)

		var parent types.BlockHeaderHash
		for n := types.BlockNumber(10); n < 13; n++ {
			block := types.NewBlock(lane, n, parent, types.GenPayload(rng))
			signed := types.Sign(keys[0], types.NewLaneProposal(block))
			parent = block.Header().Hash()
			require.NoError(t, bp.PersistBlock(signed))
		}

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		got, ok := state.LastAppQC().Get()
		require.True(t, ok)
		require.Equal(t, roadIdx, got.Proposal().RoadIndex())

		require.Equal(t, types.BlockNumber(13), state.NextBlock(lane))
		require.Equal(t, roadIdx, state.FirstCommitQC())
	})

	t.Run("headers returns ErrPruned for blocks before loaded range", func(t *testing.T) {
		dir := t.TempDir()
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())

		bp, _, err := persist.NewBlockPersister(dir)
		require.NoError(t, err)

		// Persist blocks 5-7 on lane0 directly to disk.
		lane := keys[0].Public()
		var parent types.BlockHeaderHash
		for n := types.BlockNumber(5); n < 8; n++ {
			b := testSignedBlock(keys[0], lane, n, parent, rng)
			parent = b.Msg().Block().Header().Hash()
			require.NoError(t, bp.PersistBlock(b))
		}

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		// Build a LaneRange [0, 3) — entirely before the loaded blocks.
		fakeBlock := testSignedBlock(keys[0], lane, 2, types.BlockHeaderHash{}, rng)
		lr := types.NewLaneRange(lane, 0, utils.Some(fakeBlock.Msg().Block().Header()))

		// headers() should return ErrPruned immediately (not hang) because
		// the votes queue was advanced past block 0 on restart.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err = state.headers(ctx, lr)
		require.ErrorIs(t, err, data.ErrPruned)
	})

	t.Run("loads persisted commitQCs", func(t *testing.T) {
		dir := t.TempDir()
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())

		// Persist CommitQCs to disk.
		cp, _, err := persist.NewCommitQCPersister(dir)
		require.NoError(t, err)

		qcs := make([]*types.CommitQC, 3)
		prev := utils.None[*types.CommitQC]()
		for i := range qcs {
			qcs[i] = makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qcs[i])
			require.NoError(t, cp.PersistCommitQC(qcs[i]))
		}

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		// All 3 commitQCs should be loaded (no AppQC to skip past).
		require.Equal(t, types.RoadIndex(0), state.FirstCommitQC())
		// LastCommitQC should be set to the last loaded one.
		latest, ok := state.LastCommitQC().Load().Get()
		require.True(t, ok)
		require.NoError(t, utils.TestDiff(qcs[2], latest))
	})

	t.Run("loads persisted commitQCs with AppQC", func(t *testing.T) {
		dir := t.TempDir()
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())

		// Persist AppQC at road index 1.
		roadIdx := types.RoadIndex(1)
		globalNum := types.GlobalBlockNumber(5)
		appProposal := types.NewAppProposal(globalNum, roadIdx, types.GenAppHash(rng))
		appQC := types.NewAppQC(makeAppVotes(keys, appProposal))

		persister, _, err := persist.NewPersister[*apb.AppQC](dir, innerFile)
		require.NoError(t, err)
		require.NoError(t, persister.Persist(types.AppQCConv.Encode(appQC)))

		// Persist CommitQCs 0-4.
		cp, _, err := persist.NewCommitQCPersister(dir)
		require.NoError(t, err)

		qcs := make([]*types.CommitQC, 5)
		prev := utils.None[*types.CommitQC]()
		for i := range qcs {
			qcs[i] = makeCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
			prev = utils.Some(qcs[i])
			require.NoError(t, cp.PersistCommitQC(qcs[i]))
		}

		state, err := NewState(keys[0], ds, utils.Some(dir))
		require.NoError(t, err)

		// inner.prune(appQC@1, commitQC@1) sets commitQCs.first = 1.
		require.Equal(t, types.RoadIndex(1), state.FirstCommitQC())
		latest, ok := state.LastCommitQC().Load().Get()
		require.True(t, ok)
		require.NoError(t, utils.TestDiff(qcs[4], latest))
	})

	t.Run("corrupt AppQC data returns error", func(t *testing.T) {
		dir := t.TempDir()
		ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())

		// Write a valid PersistedWrapper whose Data payload is garbage.
		// This simulates corruption at the application data level while
		// keeping the outer A/B wrapper intact.
		seq := uint64(1)
		wrapper := &apb.PersistedWrapper{Seq: &seq, Data: []byte("not a valid protobuf")}
		bz, err := proto.Marshal(wrapper)
		require.NoError(t, err)
		require.NoError(t, persist.WriteRawFile(dir, innerFile, bz))

		_, err = NewState(keys[0], ds, utils.Some(dir))
		require.Error(t, err)
	})
}
