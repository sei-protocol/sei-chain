package avail

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
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
		state := NewState(keys[0], ds)
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
	state := NewState(keys[0], ds)
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
		require.Contains(err.Error(), "mismatched QCs")
	})
}

func TestPushBlockRejectsBadParentHash(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)

	ds := data.NewState(&data.Config{
		Committee: committee,
	}, utils.None[data.BlockStore]())
	state := NewState(keys[0], ds)

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
	state := NewState(keys[0], ds)

	// Create a block on keys[0]'s lane but sign it with keys[1].
	lane := keys[0].Public()
	block := types.NewBlock(lane, 0, types.GenBlockHeaderHash(rng), types.GenPayload(rng))
	prop := types.Sign(keys[1], types.NewLaneProposal(block))

	err := state.PushBlock(ctx, prop)
	require.Error(t, err)
}
