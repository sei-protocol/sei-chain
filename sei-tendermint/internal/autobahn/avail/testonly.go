package avail

import (
	"context"
	"errors"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
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
							if errors.Is(err, data.ErrPruned) {
								next = from.FirstCommitQC()
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
			s.Spawn(func() error {
				next := types.RoadIndex(0)
				for {
					appQC, commitQC, err := from.WaitForAppQC(ctx, next)
					if err != nil {
						return err
					}
					next = appQC.Next()
					for _, to := range states {
						if err := to.PushAppQC(appQC, commitQC); err != nil {
							return err
						}
					}
				}
			})
		}
		return nil
	})
}
