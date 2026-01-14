package data

import (
	"context"
	"fmt"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/autobahn/pb"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

// Server implements pb.StreamAPIServer.
type server struct {
	state *State
}

// Register registers DataAPIServer with the given grpc server.
func (s *State) Register(grpcServer *grpc.Server) {
	pb.RegisterDataAPIServer(grpcServer, &server{state: s})
}

// StreamCommitQCs implements pb.StreamAPIServer.
// Streams local commit QCs starting from the requested index.
func (s *server) StreamFullCommitQCs(
	req *pb.StreamFullCommitQCsReq,
	stream grpc.ServerStreamingServer[pb.FullCommitQC],
) error {
	ctx := stream.Context()
	prev := utils.None[*types.FullCommitQC]()
	for i := types.GlobalBlockNumber(req.NextBlock); ; i++ {
		qc, err := s.state.QC(ctx, i)
		if err != nil {
			return fmt.Errorf("s.state.QC(): %w", err)
		}
		// Don't send the same QC twice.
		if types.NextIndexOpt(prev) > qc.Index() {
			continue
		}
		prev = utils.Some(qc)
		if err := stream.Send(types.FullCommitQCConv.Encode(qc)); err != nil {
			return fmt.Errorf("stream.Send(): %w", err)
		}
	}
}

// GetBlock implements pb.StreaAPIServer.
// Returns the requested block or an error if not found.
func (s *server) GetBlock(ctx context.Context, req *pb.GetBlockReq) (*pb.Block, error) {
	block, err := s.state.TryBlock(types.GlobalBlockNumber(req.GlobalNumber))
	if err != nil {
		return nil, fmt.Errorf("data.TryBlock(): %w", err)
	}
	return types.BlockConv.Encode(block), nil
}
