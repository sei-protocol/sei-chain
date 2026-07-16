package consensus

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func RunTestNetwork(ctx context.Context, states []*State) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			availStates := make([]*avail.State, len(states))
			for i, state := range states {
				availStates[i] = state.Avail()
			}
			return avail.RunTestNetwork(ctx, availStates)
		})
		for _, from := range states {
			for _, to := range states {
				s.Spawn(func() error {
					return from.SubscribeProposal().Iter(ctx, func(ctx context.Context, msg utils.Option[*types.FullProposal]) error {
						if proposal, ok := msg.Get(); ok {
							if err := to.PushProposal(ctx, proposal); err != nil {
								return err
							}
						}
						return nil
					})
				})
				s.Spawn(func() error {
					return from.SubscribeTimeoutQC().Iter(ctx, func(ctx context.Context, msg utils.Option[*types.TimeoutQC]) error {
						if qc, ok := msg.Get(); ok {
							return to.PushTimeoutQC(ctx, qc)
						}
						return nil
					})
				})
			}
			s.Spawn(func() error {
				return from.SubscribePrepareVote().Iter(ctx, func(_ context.Context, msg utils.Option[*types.ConsensusReqPrepareVote]) error {
					if vote, ok := msg.Get(); ok {
						for _, to := range states {
							if err := to.PushPrepareVote(vote.Signed); err != nil {
								return err
							}
						}
					}
					return nil
				})
			})
			s.Spawn(func() error {
				return from.SubscribeCommitVote().Iter(ctx, func(_ context.Context, msg utils.Option[*types.ConsensusReqCommitVote]) error {
					vote, ok := msg.Get()
					if !ok {
						return nil
					}
					for _, to := range states {
						if err := to.PushCommitVote(vote.Signed); err != nil {
							return err
						}
					}
					return nil
				})
			})
			s.Spawn(func() error {
				return from.SubscribeTimeoutVote().Iter(ctx, func(_ context.Context, msg utils.Option[*types.FullTimeoutVote]) error {
					if vote, ok := msg.Get(); ok {
						for _, to := range states {
							if err := to.PushTimeoutVote(vote); err != nil {
								return err
							}
						}
					}
					return nil
				})
			})
		}
		return nil
	})
}
