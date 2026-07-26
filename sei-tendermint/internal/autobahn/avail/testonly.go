package avail

import (
	"context"
	"errors"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func RunTestNetwork(ctx context.Context, states []*State) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for _, from := range states {
			for _, to := range states {
				s.Spawn(func() error {
					sub := from.SubscribeLaneProposals(0)
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
								next = from.FirstCommitQC()
								continue
							}
							return err
						}
						if err := to.PushCommitQC(ctx, qc); err != nil {
							return err
						}
						// PushCommitQC may no-op (stale / not yet Current) and still
						// return nil. Only advance once to's tip covers this road so
						// we do not skip an index that was never admitted.
						if err := to.waitForCommitQC(ctx, qc.Index()); err != nil {
							return err
						}
						next = qc.Index() + 1
					}
				})
			}
			s.Spawn(func() error {
				next := types.RoadIndex(0)
				for {
					appQC, commitQC, err := from.WaitForAppQC(ctx, next)
					if err != nil {
						return err
					}
					next = appQC.Next()
					for _, to := range states {
						if err := to.PushAppQC(ctx, appQC, commitQC); err != nil {
							return err
						}
					}
				}
			})
		}
		return nil
	})
}
