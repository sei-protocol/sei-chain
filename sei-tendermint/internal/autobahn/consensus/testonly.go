package consensus

import (
	"context"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// TestMinRoadBlockInterval is the consensus interval used by general test fixtures.
const TestMinRoadBlockInterval = 50 * time.Millisecond

func RunTestConn(ctx context.Context, from *State, to *State) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
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
		s.Spawn(func() error {
			return from.SubscribePrepareVote().Iter(ctx, func(ctx context.Context, msg utils.Option[*types.ConsensusMsgPrepareVote]) error {
				if vote, ok := msg.Get(); ok {
					return to.PushPrepareVote(ctx, vote.Signed)
				}
				return nil
			})
		})
		s.Spawn(func() error {
			return from.SubscribeCommitVote().Iter(ctx, func(ctx context.Context, msg utils.Option[*types.ConsensusMsgCommitVote]) error {
				if vote, ok := msg.Get(); ok {
					return to.PushCommitVote(ctx, vote.Signed)
				}
				return nil
			})
		})
		s.Spawn(func() error {
			return from.SubscribeTimeoutVote().Iter(ctx, func(ctx context.Context, msg utils.Option[*types.FullTimeoutVote]) error {
				if vote, ok := msg.Get(); ok {
					return to.PushTimeoutVote(ctx, vote)
				}
				return nil
			})
		})
		return nil
	})
}

func RunTestNetwork(ctx context.Context, states []*State) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for _, from := range states {
			for _, to := range states {
				s.Spawn(func() error { return RunTestConn(ctx, from, to) })
				s.Spawn(func() error { return avail.RunTestConn(ctx, from.Avail(), to.Avail()) })
			}
		}
		return nil
	})
}
