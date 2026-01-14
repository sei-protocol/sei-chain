package avail

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/sei-protocol/sei-stream/data"
	"github.com/sei-protocol/sei-stream/pkg/service"
	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/types"
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

func makeCommitQC(
	rng *rand.Rand,
	committee *types.Committee,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
	laneQCs map[types.LaneID]*types.LaneQC,
	appQC utils.Option[*types.AppQC],
) *types.CommitQC {
	fullProposal, err := types.NewProposal(
		types.TestSecretKey(types.GenNodeID(rng)),
		committee,
		types.ViewSpec{CommitQC: prev},
		time.Now(),
		laneQCs,
		appQC,
	)
	if err != nil {
		panic(err)
	}
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

	if err := service.Run(ctx, func(ctx context.Context, s service.Scope) error {
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
				lane := keys[rng.Intn(len(keys))].Public()
				p := types.GenPayload(rng)
				want[lane] = append(want[lane], p.Hash())
				b, err := state.ProduceBlock(ctx, lane, p)
				if err != nil {
					return fmt.Errorf("state.ProduceBlock(): %w", err)
				}
				if err := utils.TestDiff(b.Payload(), p); err != nil {
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
					for _, vote := range makeLaneVotes(keys, b.Header()) {
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
