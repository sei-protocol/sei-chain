package avail

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
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
					next := types.RoadIndex(0)
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

func seekRoads(s *State, idx types.RoadIndex) {
	for inner, ctrl := range s.inner.Lock() {
		inner.roads.first = idx
		inner.roads.next = idx
		ctrl.Updated()
	}
}

func setRoadAppQC(s *State, idx types.RoadIndex, appQC *types.AppQC) {
	for inner, ctrl := range s.inner.Lock() {
		r := inner.roads.q[idx]
		r.appQC = utils.Some(appQC)
		// Tests inject road AppQCs without going through data's persist/Anchor
		// pipeline; stamp the Anchor watermark to the road's epoch so the prune
		// leash sees the same coverage production would after one flush.
		inner.anchorEpoch = utils.Some(r.epoch)
		ctrl.Updated()
	}
}

func tipLink(ep *types.Epoch, key types.SecretKey, idx types.RoadIndex) *types.CommitQC {
	return types.NewCommitQC([]*types.Signed[*types.CommitVote]{
		types.Sign(key, types.NewCommitVote(types.ProposalAt(ep, types.View{Index: idx, Number: 0}, ep.FirstBlock()))),
	})
}

// DriveAdvance seals each applied epoch through want-1 and waits for
// runEpochAdvance to install want. The registry must already contain want;
// runEpochAdvance must be running.
// Intended for tests only.
func DriveAdvance(ctx context.Context, state *State, keys []types.SecretKey, want types.EpochIndex) error {
	for state.Epoch().Load().EpochIndex() < want {
		cur := state.Epoch().Load()
		last := cur.RoadRange().Next - 1
		if cur.RoadRange().Next == utils.Max[types.RoadIndex]() {
			return fmt.Errorf("DriveAdvance: cannot seal open road range at epoch %d", cur.EpochIndex())
		}
		cks := make([]types.SecretKey, 0, len(keys))
		for _, k := range keys {
			if cur.Committee().HasReplica(k.Public()) {
				cks = append(cks, k)
			}
		}
		if len(cks) == 0 {
			return fmt.Errorf("DriveAdvance: no committee keys for epoch %d", cur.EpochIndex())
		}
		seekRoads(state, last)
		var qc *types.CommitQC
		if last == 0 {
			qc = types.BuildCommitQC(cur, cks, utils.None[*types.CommitQC](), nil)
		} else {
			qc = types.BuildCommitQC(cur, cks, utils.Some(tipLink(cur, cks[0], last-1)), nil)
		}
		if qc.Index() != last {
			return fmt.Errorf("DriveAdvance: qc index %d != last %d", qc.Index(), last)
		}
		if err := state.PushCommitQC(ctx, qc); err != nil {
			return err
		}
		setRoadAppQC(state, qc.Index(), data.TestAppQC(cks, types.NewAppProposal(qc.Proposal(), types.AppHash{})))
		if _, err := state.Epoch().Wait(ctx, func(ep *types.Epoch) bool {
			return ep.EpochIndex() > cur.EpochIndex()
		}); err != nil {
			return err
		}
	}
	if got := state.Epoch().Load().EpochIndex(); got < want {
		return fmt.Errorf("DriveAdvance: epoch %d < want %d", got, want)
	}
	return nil
}
