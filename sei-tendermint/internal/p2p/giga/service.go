package giga

import (
	"context"

	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/internal/autobahn/data"
	"github.com/tendermint/tendermint/internal/autobahn/consensus"
	"github.com/tendermint/tendermint/internal/autobahn/avail"
	"github.com/tendermint/tendermint/internal/p2p/rpc"
)

type Service struct {
	getBlockReqs chan req
	data *data.State
	avail *avail.State
	consensus *consensus.State
}

func (x *Service) Run(ctx context.Context) error {
	return x.runBlockFetcher(ctx)
}

func (x *Service) RunServer(ctx context.Context, server rpc.Server[API]) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.serverPing(ctx,server) })
		s.Spawn(func() error { return x.serverConsensus(ctx,server) })
		s.Spawn(func() error { return x.serverStreamFullCommitQCs(ctx,server) })
		s.Spawn(func() error { return x.serverGetBlock(ctx,server) })
		s.Spawn(func() error { return x.serverStreamLaneProposals(ctx,server) })
		s.Spawn(func() error { return x.serverStreamLaneVotes(ctx,server) })
		s.Spawn(func() error { return x.serverStreamCommitQCs(ctx,server) })
		s.Spawn(func() error { return x.serverStreamAppVotes(ctx,server) })
		s.Spawn(func() error { return x.serverStreamAppQCs(ctx,server) })
		return nil
	})
}

func (x *Service) RunClient(ctx context.Context, client rpc.Client[API]) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.clientPing(ctx,client) })
		s.Spawn(func() error { return x.clientConsensus(ctx,client) })
		s.Spawn(func() error { return x.clientStreamFullCommitQCs(ctx,client) })
		s.Spawn(func() error { return x.clientGetBlock(ctx,client) })
		s.Spawn(func() error { return x.clientStreamLaneProposals(ctx,client) })
		s.Spawn(func() error { return x.clientStreamLaneVotes(ctx,client) })
		s.Spawn(func() error { return x.clientStreamCommitQCs(ctx,client) })
		s.Spawn(func() error { return x.clientStreamAppVotes(ctx,client) })
		s.Spawn(func() error { return x.clientStreamAppQCs(ctx,client) })
		return nil
	})
}
