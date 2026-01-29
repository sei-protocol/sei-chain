package consensus

import (
	"fmt"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

// Server implements Consensus and Ping RPCs of the protocol.StreamAPIServer.
type server struct {
	state *State
}

// Register registers StreamAPIServer with the given grpc server.
func (s *State) Register(grpcServer *grpc.Server) {
	s.avail.Register(grpcServer)
}

// Ping implements protocol.StreamAPIServer.
// Note that we use streaming RPC, because unary RPC apparently causes 10ms extra delay on avg (empirically tested).
func (s *server) Ping(stream grpc.BidiStreamingServer[protocol.PingReq, protocol.PingResp]) error {
	for {
		if _, err := stream.Recv(); err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		if err := stream.Send(&protocol.PingResp{}); err != nil {
			return fmt.Errorf("stream.Send(): %w", err)
		}
	}
}

// Consensus implements protocol.StreaAPIServer.
func (s *server) Consensus(stream grpc.BidiStreamingServer[protocol.ConsensusReq, protocol.ConsensusResp]) error {
	ctx := stream.Context()
	for {
		reqRaw, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		req, err := types.ConsensusReqConv.DecodeReq(reqRaw)
		if err != nil {
			return fmt.Errorf("types.SignedMsgConv.DecodeReq(): %w", err)
		}
		switch req := req.(type) {
		case *types.ConsensusReqPrepareVote:
			if err := s.state.PushPrepareVote(req.Signed); err != nil {
				return fmt.Errorf("s.state.PushPrepareVote(): %w", err)
			}
		case *types.ConsensusReqCommitVote:
			if err := s.state.PushCommitVote(req.Signed); err != nil {
				return fmt.Errorf("s.state.PushCommitVote(): %w", err)
			}
		case *types.FullTimeoutVote:
			if err := s.state.PushTimeoutVote(req); err != nil {
				return fmt.Errorf("s.state.PushTimeoutVote(): %w", err)
			}
		case *types.FullProposal:
			if err := s.state.PushProposal(ctx, req); err != nil {
				return fmt.Errorf("s.state.PushProposal(): %w", err)
			}
		case *types.TimeoutQC:
			if err := s.state.PushTimeoutQC(ctx, req); err != nil {
				return fmt.Errorf("s.state.PushTimeoutQC(): %w", err)
			}
		default:
			return fmt.Errorf("unknown consensus request type: %T", req)
		}
		if err := stream.Send(&protocol.ConsensusResp{}); err != nil {
			return fmt.Errorf("stream.Send(): %w", err)
		}
	}
}
