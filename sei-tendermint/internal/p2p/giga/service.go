package giga

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// Service serves the giga RPC API. NewService builds a full validator
// service (all streams); NewBlockSyncService builds a block-sync-only
// service (StreamFullCommitQCs + GetBlock) for fullnodes. state is nil on
// block-sync-only services and is only dereferenced by handlers spawned
// from the validator-only RunServer / RunClient entry points.
type Service struct {
	getBlockReqs chan req
	data         *data.State
	state        *consensus.State
}

func NewService(state *consensus.State) *Service {
	return &Service{
		getBlockReqs: make(chan req),
		data:         state.Data(),
		state:        state,
	}
}

// NewBlockSyncService constructs a Service that only serves and consumes
// block-sync streams (no consensus / avail).
func NewBlockSyncService(d *data.State) *Service {
	return &Service{
		getBlockReqs: make(chan req),
		data:         d,
	}
}

func (x *Service) Run(ctx context.Context) error {
	return x.runBlockFetcher(ctx)
}

func (x *Service) RunServer(ctx context.Context, server rpc.Server[API]) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.serverPing(ctx, server) })
		s.Spawn(func() error { return x.serverConsensus(ctx, server) })
		s.Spawn(func() error { return x.serverStreamFullCommitQCs(ctx, server) })
		s.Spawn(func() error { return x.serverGetBlock(ctx, server) })
		s.Spawn(func() error { return x.serverStreamLaneProposals(ctx, server) })
		s.Spawn(func() error { return x.serverStreamLaneVotes(ctx, server) })
		s.Spawn(func() error { return x.serverStreamCommitQCs(ctx, server) })
		s.Spawn(func() error { return x.serverStreamAppVotes(ctx, server) })
		s.Spawn(func() error { return x.serverStreamAppQCs(ctx, server) })
		return nil
	})
}

func (x *Service) RunClient(ctx context.Context, client rpc.Client[API]) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.clientPing(ctx, client) })
		s.Spawn(func() error { return x.clientConsensus(ctx, client) })
		s.Spawn(func() error { return x.clientStreamFullCommitQCs(ctx, client) })
		s.Spawn(func() error { return x.clientGetBlock(ctx, client) })
		s.Spawn(func() error { return x.clientStreamLaneProposals(ctx, client) })
		s.Spawn(func() error { return x.clientStreamLaneVotes(ctx, client) })
		s.Spawn(func() error { return x.clientStreamCommitQCs(ctx, client) })
		s.Spawn(func() error { return x.clientStreamAppVotes(ctx, client) })
		s.Spawn(func() error { return x.clientStreamAppQCs(ctx, client) })
		return nil
	})
}

// RunBlockSyncServer spawns only the block-sync server handlers. Used by
// validators on inbound connections from non-committee peers.
func (x *Service) RunBlockSyncServer(ctx context.Context, server rpc.Server[API]) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.serverPing(ctx, server) })
		s.Spawn(func() error { return x.serverStreamFullCommitQCs(ctx, server) })
		s.Spawn(func() error { return x.serverGetBlock(ctx, server) })
		return nil
	})
}

// RunBlockSyncClient spawns only the block-sync client handlers. Used by
// fullnodes dialing committee members.
func (x *Service) RunBlockSyncClient(ctx context.Context, client rpc.Client[API]) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.clientPing(ctx, client) })
		s.Spawn(func() error { return x.clientStreamFullCommitQCs(ctx, client) })
		s.Spawn(func() error { return x.clientGetBlock(ctx, client) })
		return nil
	})
}
