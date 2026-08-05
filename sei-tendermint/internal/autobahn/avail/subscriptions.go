package avail

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"golang.org/x/sync/errgroup"
)

// errGotProposal cancels the LocalLane waiter once Block succeeds.
var errGotProposal = errors.New("got proposal")

// SubscribeLaneProposals streams proposals for this node's pubkey, bound to the
// applied LaneID at subscribe time. Stay across epochs (pubkey still in
// committee) keeps the same LaneID and must not restart the stream. Leave
// (LocalLane → None) ends the stream with ErrLaneIdentityChanged; callers
// recreate after a later rejoin. A LaneID change without leave is impossible
// under contiguous ActivateCommittee stay and panics.
func (s *State) SubscribeLaneProposals(first types.BlockNumber) (*LaneProposalsRecv, error) {
	lane, ok := s.LocalLane().Get()
	if !ok {
		return nil, ErrBadLane
	}
	return &LaneProposalsRecv{s, lane, first}, nil
}

type LaneProposalsRecv struct {
	state *State
	lane  types.LaneID
	next  types.BlockNumber
}

// checkBound returns nil while LocalLane is still this streak, ErrLaneIdentityChanged
// on leave, and panics if the pubkey's LaneID changed without a leave.
func (r *LaneProposalsRecv) checkBound(opt utils.Option[types.LaneID]) error {
	got, ok := opt.Get()
	if !ok {
		return ErrLaneIdentityChanged
	}
	if got != r.lane {
		panic(fmt.Sprintf(
			"SubscribeLaneProposals: LaneID changed without leave: bound %v got %v",
			r.lane, got,
		))
	}
	return nil
}

func (r *LaneProposalsRecv) Recv(ctx context.Context) (*types.Signed[*types.LaneProposal], error) {
	for {
		if err := r.checkBound(r.state.LocalLane()); err != nil {
			return nil, err
		}
		g, gctx := errgroup.WithContext(ctx)
		var proposal *types.Signed[*types.LaneProposal]
		g.Go(func() error {
			for {
				b, err := r.state.Block(gctx, r.lane, r.next)
				if err != nil {
					if errors.Is(err, types.ErrPruned) {
						r.next += 1
						continue
					}
					return fmt.Errorf("x.avail.Block(): %w", err)
				}
				proposal = b
				r.next += 1
				return errGotProposal
			}
		})
		g.Go(func() error {
			_, err := r.state.LocalLaneUpdates().Wait(gctx, func(opt utils.Option[types.LaneID]) bool {
				got, ok := opt.Get()
				return !ok || got != r.lane
			})
			if err != nil {
				return err
			}
			return r.checkBound(r.state.LocalLane())
		})
		err := g.Wait()
		if proposal != nil {
			return proposal, nil
		}
		if err != nil {
			return nil, err
		}
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
			if errors.Is(err, types.ErrPruned) {
				r.next = max(r.next+1, r.state.data.FirstAppProposal())
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
