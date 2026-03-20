package avail

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
)

func (s *State) SubscribeLaneProposals(first types.BlockNumber) *LaneProposalsRecv {
	return &LaneProposalsRecv{s, s.key.Public(), first}
}

type LaneProposalsRecv struct {
	state *State
	lane  types.LaneID
	next  types.BlockNumber
}

func (r *LaneProposalsRecv) Recv(ctx context.Context) (*types.Signed[*types.LaneProposal], error) {
	for {
		b, err := r.state.Block(ctx, r.lane, r.next)
		if err != nil {
			if errors.Is(err, data.ErrPruned) {
				r.next += 1
				continue
			}
			return nil, fmt.Errorf("x.avail.Block(): %w", err)
		}
		r.next += 1
		return b, nil
	}
}

func (s *State) SubscribeLaneVotes() *LaneVotesRecv {
	return &LaneVotesRecv{s, map[types.LaneID]types.BlockNumber{}}
}

type LaneVotesRecv struct {
	state *State
	next  map[types.LaneID]types.BlockNumber
}

func (r *LaneVotesRecv) RecvBatch(ctx context.Context) ([]*types.Signed[*types.LaneVote], error) {
	var batch []*types.BlockHeader
	for inner, ctrl := range r.state.inner.Lock() {
		for {
			for lane, bq := range inner.blocks {
				upperBound := min(bq.next, inner.nextBlockToPersist[lane])
				for i := max(bq.first, r.next[lane]); i < upperBound; i++ {
					batch = append(batch, bq.q[i].Msg().Block().Header())
				}
				r.next[lane] = upperBound
			}
			if len(batch) > 0 {
				break
			}
			if err := ctrl.Wait(ctx); err != nil {
				return nil, err
			}
		}
	}
	// TODO(gprusak): we sign the votes per VotesRecv instance, which is suboptimal.
	// We should sign votes as soon as blocks arrive and cache them (in PushBlock and ProduceBlock).
	votes := make([]*types.Signed[*types.LaneVote], len(batch))
	for i, h := range batch {
		votes[i] = types.Sign(r.state.key, types.NewLaneVote(h))
	}
	return votes, nil
}

type AppVotesRecv struct {
	state *State
	next  types.GlobalBlockNumber
}

func (s *State) SubscribeAppVotes() *AppVotesRecv {
	return &AppVotesRecv{s, 0}
}

func (r *AppVotesRecv) Recv(ctx context.Context) (*types.Signed[*types.AppVote], error) {
	for {
		// If needed, fast forward to the first global number without known AppQC.
		if qc, ok := r.state.LastAppQC().Get(); ok {
			r.next = max(r.next, qc.Proposal().GlobalNumber()+1)
		}
		// Fetch the proposal.
		p, err := r.state.data.AppProposal(ctx, r.next)
		if err != nil {
			if errors.Is(err, data.ErrPruned) {
				r.next = r.next + 1
				continue
			}
			return nil, err
		}
		// AppProposal currently might return a proposal with a higher global number than the one we requested.
		// Correct the n in such a case.
		// TODO(gprusak): perhaps it would be possible to require AppHash at every block from the execution engine.
		// This would simplify the data state.
		r.next = p.GlobalNumber() + 1
		return types.Sign(r.state.key, types.NewAppVote(p)), nil
	}
}
