package giga

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// Service serves the giga RPC API. Two construction modes:
//
//   - NewService(consensusState) — full validator service: serves and consumes
//     all 9 streams (block-sync + consensus + avail).
//   - NewBlockSyncService(dataState) — block-sync only: serves and consumes
//     just StreamFullCommitQCs + GetBlock. Used by rpc-only nodes that hold
//     data.State but not the consensus / avail layers.
//
// All block-sync methods read/write s.data directly; consensus/avail methods
// dereference s.state, which is nil in block-sync-only mode (callers must not
// reach those methods on an rpc-only service).
type Service struct {
	getBlockReqs chan req
	data         *data.State
	// state is the validator-mode consensus state. nil in block-sync-only mode.
	state *consensus.State
}

func NewService(state *consensus.State) *Service {
	return &Service{
		getBlockReqs: make(chan req),
		data:         state.Data(),
		state:        state,
	}
}

// NewBlockSyncService constructs a Service that only serves and consumes
// block-sync streams. Used by rpc-only nodes which sync finalized blocks from
// validators but don't run consensus or avail themselves.
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

// RunBlockSyncServer spawns only the block-sync server handlers. Validators
// call this on inbound connections from rpc-only (non-committee) peers, so
// rpc-only peers can pull finalized blocks but can't push consensus messages
// or observe consensus/avail subscriptions.
func (x *Service) RunBlockSyncServer(ctx context.Context, server rpc.Server[API]) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.serverPing(ctx, server) })
		s.Spawn(func() error { return x.serverStreamFullCommitQCs(ctx, server) })
		s.Spawn(func() error { return x.serverGetBlock(ctx, server) })
		return nil
	})
}

// RunBlockSyncClient spawns only the block-sync client handlers. Rpc-only
// nodes call this when dialing each validator, pulling FullCommitQCs and
// block payloads and feeding them into the local data.State via PushQC /
// PushBlock.
func (x *Service) RunBlockSyncClient(ctx context.Context, client rpc.Client[API]) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.clientPing(ctx, client) })
		s.Spawn(func() error { return x.clientStreamFullCommitQCs(ctx, client) })
		s.Spawn(func() error { return x.clientGetBlock(ctx, client) })
		return nil
	})
}
