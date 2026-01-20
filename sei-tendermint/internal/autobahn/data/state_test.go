package data

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/tendermint/tendermint/internal/autobahn/types"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
)

type Snapshot struct {
	Blocks       map[types.GlobalBlockNumber]*types.Block
	QCs          map[types.GlobalBlockNumber]*types.FullCommitQC
	AppProposals map[types.GlobalBlockNumber]*types.AppProposal
}

func newSnapshot() Snapshot {
	return Snapshot{
		Blocks:       map[types.GlobalBlockNumber]*types.Block{},
		QCs:          map[types.GlobalBlockNumber]*types.FullCommitQC{},
		AppProposals: map[types.GlobalBlockNumber]*types.AppProposal{},
	}
}

func snapshot(s *State) Snapshot {
	for inner := range s.inner.Lock() {
		aps := map[types.GlobalBlockNumber]*types.AppProposal{}
		for n, apt := range inner.appProposals {
			aps[n] = apt.proposal
		}
		return Snapshot{
			QCs:          maps.Clone(inner.qcs),
			Blocks:       maps.Clone(inner.blocks),
			AppProposals: aps,
		}
	}
	panic("unreachable")
}

func makeAppQC(keys []types.SecretKey, proposal *types.AppProposal) *types.AppQC {
	vote := types.NewAppVote(proposal)
	var votes []*types.Signed[*types.AppVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewAppQC(votes)
}

func makeLaneQC(keys []types.SecretKey, header *types.BlockHeader) *types.LaneQC {
	vote := types.NewLaneVote(header)
	votes := []*types.Signed[*types.LaneVote]{}
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewLaneQC(votes)
}

func makeCommitQC(
	rng utils.Rng,
	committee *types.Committee,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
) (*types.FullCommitQC, []*types.Block) {
	blocks := map[types.LaneID][]*types.Block{}
	makeBlock := func(producer types.LaneID) *types.Block {
		if bs := blocks[producer]; len(bs) > 0 {
			parent := bs[len(bs)-1]
			return types.NewBlock(
				producer,
				parent.Header().Next(),
				parent.Header().Hash(),
				types.GenPayload(rng),
			)
		}
		return types.NewBlock(
			producer,
			types.LaneRangeOpt(prev, producer).Next(),
			types.GenBlockHeaderHash(rng),
			types.GenPayload(rng),
		)
	}
	// Make some blocks
	for range 10 {
		producer := committee.Lanes().At(rng.Intn(committee.Lanes().Len()))
		blocks[producer] = append(blocks[producer], makeBlock(producer))
	}
	// Construct a proposal.
	laneQCs := map[types.LaneID]*types.LaneQC{}
	var headers []*types.BlockHeader
	var blockList []*types.Block
	for _, lane := range committee.Lanes().All() {
		if bs := blocks[lane]; len(bs) > 0 {
			laneQCs[lane] = makeLaneQC(keys, bs[len(bs)-1].Header())
			for _, b := range bs {
				headers = append(headers, b.Header())
				blockList = append(blockList, b)
			}
		}
	}
	viewSpec := types.ViewSpec{CommitQC: prev}
	proposal, err := types.NewProposal(
		types.GenSecretKey(rng),
		committee,
		viewSpec,
		time.Now(),
		laneQCs,
		func() utils.Option[*types.AppQC] {
			if n := types.GlobalRangeOpt(prev).Next; n > 0 {
				p := types.NewAppProposal(n-1, viewSpec.View().Index, types.GenAppHash(rng))
				return utils.Some(makeAppQC(keys, p))
			}
			return utils.None[*types.AppQC]()
		}(),
	)
	if err != nil {
		panic(err)
	}
	var votes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewCommitVote(proposal.Proposal().Msg())))
	}
	return types.NewFullCommitQC(
		types.NewCommitQC(votes),
		headers,
	), blockList
}

func TestState(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		state := NewState(&Config{
			Committee: committee,
		}, utils.None[BlockStore]())
		s.SpawnBgNamed("state.Run()", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		want := newSnapshot()
		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)
			qc, blocks := makeCommitQC(rng, committee, keys, prev)
			prev = utils.Some(qc.QC())
			if err := state.PushQC(ctx, qc, blocks); err != nil {
				return fmt.Errorf("state.PushQC(): %w", err)
			}
			gr := qc.QC().GlobalRange()
			for n := gr.First; n < gr.Next; n += 1 {
				want.QCs[n] = qc
				want.Blocks[n] = blocks[n-gr.First]
			}
			if err := utils.TestDiff(want, snapshot(state)); err != nil {
				return fmt.Errorf("snapshot: %w", err)
			}
		}
		for n, wantB := range want.Blocks {
			gotB, err := state.Block(ctx, n)
			if err != nil {
				return fmt.Errorf("state.Block(%v): %w", n, err)
			}
			if err := utils.TestDiff(wantB, gotB); err != nil {
				return fmt.Errorf("state.Block(%v): %w", n, err)
			}

			gotB, err = state.TryBlock(n)
			if err != nil {
				return fmt.Errorf("state.TryBlock(%v): %w", n, err)
			}
			if err := utils.TestDiff(wantB, gotB); err != nil {
				return fmt.Errorf("state.TryBlock(%v): %w", n, err)
			}

			wantG := &types.GlobalBlock{
				GlobalNumber:  n,
				Header:        wantB.Header(),
				Payload:       wantB.Payload(),
				FinalAppState: want.QCs[n].QC().Proposal().App(),
			}
			gotG, err := state.GlobalBlock(ctx, n)
			if err != nil {
				return fmt.Errorf("state.GlobalBlock(%v): %w", n, err)
			}
			if err := utils.TestDiff(wantG, gotG); err != nil {
				return fmt.Errorf("state.GlobalBlock(%v): %w", n, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestExecution(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		state := NewState(&Config{
			Committee: committee,
		}, utils.None[BlockStore]())
		s.SpawnBgNamed("state.Run()", func() error {
			return utils.IgnoreCancel(state.Run(ctx))
		})

		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)
			qc, blocks := makeCommitQC(rng, committee, keys, prev)
			if err := state.PushQC(ctx, qc, blocks); err != nil {
				return fmt.Errorf("state.PushQC(): %w", err)
			}
			prev = utils.Some(qc.QC())
			gr := qc.QC().GlobalRange()
			if err := state.PushAppHash(gr.Next, types.GenAppHash(rng)); err == nil {
				return errors.New("PushAppProposal expected to fail on non-finalized blocks")
			}
			for n := gr.First; n < gr.Next; n += 1 {
				if err := state.PushAppHash(n, types.GenAppHash(rng)); err != nil {
					return fmt.Errorf("state.PushAppProposal(): %w", err)
				}
				if err := state.PushAppHash(n, types.GenAppHash(rng)); err == nil {
					return errors.New("PushAppProposal expected to fail on duplicate proposal")
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
