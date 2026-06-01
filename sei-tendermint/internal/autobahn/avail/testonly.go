package avail

import (
	"context"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func RunTestNetwork(ctx context.Context, states []*State) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for _,from := range states {
			s.Spawn(func() error {
				sub := from.SubscribeLaneProposals(0)
				for {
					p,err := sub.Recv(ctx)
					if err!=nil { return err }
					for _,to := range states {
						if err:=to.PushBlock(ctx,p); err!=nil { return err }
					}
				}
			})
			s.Spawn(func() error {
				// LaneVotes 
			})
			s.Spawn(func() error {
				// AppVotes
			})
			s.Spawn(func() error {
				// AppQCs
			})
			s.Spawn(func() error {
				// CommitQCs
			})
		}
	})
}
