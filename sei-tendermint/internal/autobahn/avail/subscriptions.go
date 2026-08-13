package avail

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// SubscribeLaneProposals binds lane for this node's validator key.
// Recv returns ErrLaneClosed after that lane's maps are dropped.
func (s *State) SubscribeLaneProposals(lane types.LaneID, first types.BlockNumber) *LaneProposalsRecv {
	if lane.Validator != s.key.Public() {
		panic(fmt.Sprintf("SubscribeLaneProposals: lane validator %v != local %v", lane.Validator, s.key.Public()))
	}
	return &LaneProposalsRecv{s, lane, first}
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
			if errors.Is(err, types.ErrPruned) {
				for inner := range r.state.inner.Lock() {
					if _, ok := inner.blocks[r.lane]; !ok {
						return nil, ErrLaneClosed
					}
				}
				r.next += 1
				continue
			}
			return nil, fmt.Errorf("x.avail.Block(): %w", err)
		}
		r.next += 1
		return b, nil
	}
}

// WaitLane waits until the applied committee has a LaneID for pk.
// If exclude is Some, also requires the LaneID to differ (e.g. after the
// previous identity was closed and a new LaneID allocated).
func (s *State) WaitLane(ctx context.Context, pk types.PublicKey, exclude utils.Option[types.LaneID]) (types.LaneID, error) {
	ep, err := s.epoch.Wait(ctx, func(ep *types.Epoch) bool {
		got, ok := ep.Committee().Lane(pk).Get()
		if !ok {
			return false
		}
		if prev, has := exclude.Get(); has && got == prev {
			return false
		}
		return true
	})
	if err != nil {
		return types.LaneID{}, err
	}
	return ep.Committee().Lane(pk).OrPanic("WaitLane"), nil
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
	return &AppVotesRecv{s, s.data.NextAppQC()}
}

func (r *AppVotesRecv) Recv(ctx context.Context) (*types.Signed[*types.AppVote], error) {
	for {
		vote, err := r.state.data.AppVote(ctx, r.next)
		if err != nil {
			if errors.Is(err, types.ErrPruned) {
				r.next = max(r.next, r.state.data.NextAppQC())
				continue
			}
			return nil, err
		}
		r.next = vote.Proposal().GlobalRange().Next
		return types.Sign(r.state.key, vote), nil
	}
}
