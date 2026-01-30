package giga 

import (
	"fmt"
	"time"
	"context"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/internal/p2p/rpc"
	"github.com/tendermint/tendermint/internal/p2p/giga/pb"
	apb "github.com/tendermint/tendermint/internal/autobahn/pb"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

// Sends a consensus message to the peer whenever atomic watch is updated.
func sendUpdates[T interface { comparable; types.ConsensusReq }](
	ctx context.Context,
	client rpc.Client[API],
	w utils.AtomicRecv[utils.Option[T]],
) error {
	stream, err := Consensus.Call(ctx, client)
	if err != nil {
		return fmt.Errorf("p.client.Consensus(): %w", err)
	}
	defer stream.Close()
	var last utils.Option[T]
	for {
		if last, err = w.Wait(ctx, func(m utils.Option[T]) bool { return m != last }); err != nil {
			return err
		}
		last, ok := last.Get()
		if !ok {
			continue
		}
		if err := stream.Send(ctx,types.ConsensusReqConv.Encode(last)); err != nil {
			return fmt.Errorf("stream.Send(): %w", err)
		}
	}
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
			stream, err := Ping.Call(ctx,client)
			if err != nil {
				return fmt.Errorf("p.client.Ping(): %w", err)
			}
			defer stream.Close()
			// TODO(gprusak): add random payload to actually verify roundtrip latency.
			if err := stream.Send(ctx,&pb.PingReq{}); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
			_, err = stream.Recv(ctx)
			if err != nil {
				return fmt.Errorf("stream.Recv(): %w", err)
			}
			//
			return nil
		}); err!=nil {
			return err
		}
	}
}

// Run sends newest consensus messages to the peer.
func (x *Service) clientConsensus(ctx context.Context, c rpc.Client[API]) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Send updates about new consensus messages.
		s.Spawn(func() error { return sendUpdates(ctx, c, x.state.SubscribeProposal()) })
		s.Spawn(func() error { return sendUpdates(ctx, c, x.state.SubscribePrepareVote()) })
		s.Spawn(func() error { return sendUpdates(ctx, c, x.state.SubscribeCommitVote()) })
		s.Spawn(func() error { return sendUpdates(ctx, c, x.state.SubscribeTimeoutVote()) })
		s.Spawn(func() error { return sendUpdates(ctx, c, x.state.SubscribeTimeoutQC()) })
		return nil
	})
}

// Ping implements pb.StreamAPIServer.
// Note that we use streaming RPC, because unary RPC apparently causes 10ms extra delay on avg (empirically tested).
func (x *Service) serverPing(ctx context.Context, server rpc.Server[API]) error {
	return Ping.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.PingResp,*pb.PingReq]) error {
		if _, err := stream.Recv(ctx); err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		if err := stream.Send(ctx,&pb.PingResp{}); err != nil {
			return fmt.Errorf("stream.Send(): %w", err)
		}
		return nil
	})
}

// Consensus implements pb.StreaAPIServer.
func (x *Service) serverConsensus(ctx context.Context, server rpc.Server[API]) error {
	return Consensus.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.ConsensusResp,*apb.ConsensusReq]) error {
		for {
			reqRaw, err := stream.Recv(ctx)
			if err != nil {
				return fmt.Errorf("stream.Recv(): %w", err)
			}
			req, err := types.ConsensusReqConv.DecodeReq(reqRaw)
			if err != nil {
				return fmt.Errorf("types.SignedMsgConv.DecodeReq(): %w", err)
			}
			switch req := req.(type) {
			case *types.ConsensusReqPrepareVote:
				if err := x.state.PushPrepareVote(req.Signed); err != nil {
					return fmt.Errorf("x.state.PushPrepareVote(): %w", err)
				}
			case *types.ConsensusReqCommitVote:
				if err := x.state.PushCommitVote(req.Signed); err != nil {
					return fmt.Errorf("x.state.PushCommitVote(): %w", err)
				}
			case *types.FullTimeoutVote:
				if err := x.state.PushTimeoutVote(req); err != nil {
					return fmt.Errorf("x.state.PushTimeoutVote(): %w", err)
				}
			case *types.FullProposal:
				if err := x.state.PushProposal(ctx, req); err != nil {
					return fmt.Errorf("x.state.PushProposal(): %w", err)
				}
			case *types.TimeoutQC:
				if err := x.state.PushTimeoutQC(ctx, req); err != nil {
					return fmt.Errorf("x.state.PushTimeoutQC(): %w", err)
				}
			default:
				return fmt.Errorf("unknown consensus request type: %T", req)
			}
		}
	})
}
