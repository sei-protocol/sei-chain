package giga

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// Sends a consensus message to the peer whenever atomic watch is updated.
func sendUpdates[T interface {
	comparable
	types.ConsensusReq
}](
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
		if err := stream.Send(ctx, types.ConsensusReqConv.Encode(last)); err != nil {
			return fmt.Errorf("stream.Send(): %w", err)
		}
	}
}

// Run sends newest consensus messages to the peer.
func (x *validatorService) clientConsensus(ctx context.Context, c rpc.Client[API]) error {
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

// Consensus implements pb.StreaAPIServer.
func (x *validatorService) serverConsensus(ctx context.Context, server rpc.Server[API]) error {
	return Consensus.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.ConsensusResp, *apb.ConsensusReq]) error {
		for {
			reqRaw, err := stream.Recv(ctx)
			if err != nil {
				return fmt.Errorf("stream.Recv(): %w", err)
			}
			req, err := types.ConsensusReqConv.DecodeReq(reqRaw)
			if err != nil {
				return fmt.Errorf("types.ConsensusReqConv.DecodeReq(): %w", err)
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
