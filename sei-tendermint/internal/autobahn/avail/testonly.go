package avail

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func RunTestNetwork(ctx context.Context, states []*State) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for _, from := range states {
			for _, to := range states {
				s.Spawn(func() error {
					lane, ok := from.LocalLane().Get()
					if !ok {
						return errors.New("SubscribeLaneProposals: no local lane")
					}
					sub := from.SubscribeLaneProposals(lane, 0)
					for {
						p, err := sub.Recv(ctx)
						if err != nil {
							return err
						}
						if err := to.PushBlock(ctx, p); err != nil {
							return err
						}
					}
				})
				s.Spawn(func() error {
					sub := from.SubscribeLaneVotes()
					for {
						batch, err := sub.RecvBatch(ctx)
						if err != nil {
							return err
						}
						for _, vote := range batch {
							if err := to.PushVote(ctx, vote); err != nil {
								return err
							}
						}
					}
				})
				s.Spawn(func() error {
					sub := from.SubscribeAppVotes()
					for {
						vote, err := sub.Recv(ctx)
						if err != nil {
							return err
						}
						if err := to.PushAppVote(ctx, vote); err != nil {
							return err
						}
					}
				})
				s.Spawn(func() error {
					next := from.First()
					for {
						qc, err := from.CommitQC(ctx, next)
						if err != nil {
							if errors.Is(err, types.ErrPruned) {
								next = from.First()
								continue
							}
							return err
						}
						next = qc.Index() + 1
						if err := to.PushCommitQC(ctx, qc); err != nil {
							return err
						}
					}
				})
			}
		}
		return nil
	})
}

func testAnchorAt(ep, next *types.Epoch, keys []types.SecretKey, index types.RoadIndex) data.Anchor {
	cks := make([]types.SecretKey, 0, len(keys))
	for _, key := range keys {
		if ep.Committee().HasReplica(key.Public()) {
			cks = append(cks, key)
		}
	}
	at := types.NewEpoch(ep.EpochIndex(), types.RoadRange{First: index, Next: ep.RoadRange().Next}, ep.Committee())
	view := types.ViewSpec{ConsensusSpec: types.ConsensusSpec{Epoch: at}}
	leader := ep.Committee().Leader(view.View())
	leaderKey := cks[0]
	for _, key := range cks {
		if key.Public() == leader {
			leaderKey = key
			break
		}
	}
	proposal := utils.OrPanic1(types.NewProposal(leaderKey, view, view.GenesisTimestamp, nil))
	prepare := types.BuildPrepareQC(cks, proposal.Proposal().Msg())
	votes := make([]*types.Signed[*types.CommitVote], len(cks))
	for n, key := range cks {
		votes[n] = types.Sign(key, types.NewCommitVote(prepare))
	}
	qc := types.NewCommitQC(votes)
	return data.Anchor{Epochs: [2]*types.Epoch{ep, next}, First: utils.Some(data.AnchorFirst{
		CommitQC: qc, AppQC: data.TestAppQC(cks, types.NewAppProposal(qc.View(), types.AppHash{})),
	})}
}

func tipLink(ep *types.Epoch, key types.SecretKey, idx types.RoadIndex) *types.CommitQC {
	voteProposal := types.ProposalAt(ep, types.View{Index: idx, Number: 0})
	return types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(key, types.NewCommitVote(types.BuildPrepareQC([]types.SecretKey{key}, voteProposal))),
	})
}

// TestAdvanceToEpoch publishes certified epoch seals through want-1.
// The registry must already contain each successor epoch.
func TestAdvanceToEpoch(ctx context.Context, state *State, keys []types.SecretKey, registry *epoch.Registry, want types.EpochIndex) error {
	for state.Epoch().Load().EpochIndex() < want {
		cur := state.Epoch().Load()
		if cur.RoadRange().Next == utils.Max[types.RoadIndex]() {
			return fmt.Errorf("TestAdvanceToEpoch: cannot seal open road range at epoch %d", cur.EpochIndex())
		}
		next, err := registry.Epoch(cur.EpochIndex() + 1)
		if err != nil {
			return err
		}
		anchor := testAnchorAt(cur, next, keys, cur.RoadRange().Next-1)
		for inner, ctrl := range state.inner.Lock() {
			inner.prune(anchor)
			ctrl.Updated()
		}
		if _, err := state.Epoch().Wait(ctx, func(ep *types.Epoch) bool { return ep.EpochIndex() > cur.EpochIndex() }); err != nil {
			return err
		}
	}
	return nil
}
