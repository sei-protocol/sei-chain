package giga

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// Service serves the giga RPC API.
type Service struct {
	getBlockReqs chan req
	data         *data.State
	state        utils.Option[*consensus.State]
}

type validatorService struct {
	state *consensus.State
}

// NewService constructs service with all streams.
func NewService(state *consensus.State) *Service {
	return &Service{
		getBlockReqs: make(chan req),
		data:         state.Data(),
		state:        utils.Some(state),
	}
}

// NewFullNodeService constructs a Service that only serves and consumes
// fullnode streams (no consensus / avail).
func NewFullNodeService(d *data.State) *Service {
	return &Service{
		getBlockReqs: make(chan req),
		data:         d,
	}
}

func (x *Service) Run(ctx context.Context) error {
	return x.runBlockFetcher(ctx)
}

func (x *validatorService) RunServer(ctx context.Context, server rpc.Server[API]) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.serverConsensus(ctx, server) })
		s.Spawn(func() error { return x.serverStreamLaneProposals(ctx, server) })
		s.Spawn(func() error { return x.serverStreamLaneVotes(ctx, server) })
		s.Spawn(func() error { return x.serverStreamCommitQCs(ctx, server) })
		s.Spawn(func() error { return x.serverStreamAppVotes(ctx, server) })
		return nil
	})
}

func (x *validatorService) RunClient(ctx context.Context, client rpc.Client[API]) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.clientConsensus(ctx, client) })
		s.Spawn(func() error { return x.clientStreamLaneProposals(ctx, client) })
		s.Spawn(func() error { return x.clientStreamLaneVotes(ctx, client) })
		s.Spawn(func() error { return x.clientStreamCommitQCs(ctx, client) })
		s.Spawn(func() error { return x.clientStreamAppVotes(ctx, client) })
		return nil
	})
}

func (x *Service) RunServer(ctx context.Context, server rpc.Server[API], isCommittee bool) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.serverPing(ctx, server) })
		s.Spawn(func() error { return x.serverStreamFullCommitQCs(ctx, server) })
		s.Spawn(func() error { return x.serverGetBlock(ctx, server) })
		s.Spawn(func() error { return x.serverStreamAppQCs(ctx, server) })
		if c, ok := x.state.Get(); ok && isCommittee {
			return (&validatorService{c}).RunServer(ctx, server)
		}
		return nil
	})
}

func (x *Service) RunClient(ctx context.Context, client rpc.Client[API], getBlock bool) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.clientPing(ctx, client) })
		s.Spawn(func() error { return x.clientStreamFullCommitQCs(ctx, client) })
		s.Spawn(func() error { return x.clientStreamAppQCs(ctx, client) })
		// TODO: implement a uniform robust GetBlock peer-selection / retry strategy
		// so connections that lack a height (including self) do not need a separate
		// getBlock=false path to avoid starving the shared fetch queue.
		if getBlock {
			s.Spawn(func() error { return x.clientGetBlock(ctx, client) })
		}
		if c, ok := x.state.Get(); ok {
			return (&validatorService{c}).RunClient(ctx, client)
		}
		return nil
	})
}

// Ping implements pb.StreamAPIServer.
// Note that we use streaming RPC, because unary RPC apparently causes 10ms extra delay on avg (empirically tested).
func (x *Service) serverPing(ctx context.Context, server rpc.Server[API]) error {
	return Ping.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.PingResp, *pb.PingReq]) error {
		if _, err := stream.Recv(ctx); err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		if err := stream.Send(ctx, &pb.PingResp{}); err != nil {
			return fmt.Errorf("stream.Send(): %w", err)
		}
		return nil
	})
}

const pingInterval = 10 * time.Second
const pingTimeout = 5 * time.Second

// sendPings periodically sends Ping messages.
func (x *Service) clientPing(ctx context.Context, client rpc.Client[API]) error {
	for {
		if err := utils.Sleep(ctx, pingInterval); err != nil {
			return err
		}
		if err := utils.WithTimeout(ctx, pingTimeout, func(ctx context.Context) error {
			stream, err := Ping.Call(ctx, client)
			if err != nil {
				return fmt.Errorf("p.client.Ping(): %w", err)
			}
			defer stream.Close()
			// TODO(gprusak): add random payload to actually verify roundtrip latency.
			if err := stream.Send(ctx, &pb.PingReq{}); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
			_, err = stream.Recv(ctx)
			if err != nil {
				return fmt.Errorf("stream.Recv(): %w", err)
			}
			//
			return nil
		}); err != nil {
			return err
		}
	}
}
