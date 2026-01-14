package producer

import (
	"errors"
	"io"

	"google.golang.org/grpc"

	"github.com/tendermint/tendermint/internal/autobahn/metrics"
	"github.com/tendermint/tendermint/internal/autobahn/pb"
)

// Server implements pb.ProducerAPIServer.
type server struct {
	state *State
}

// Register registers ProducerAPIServer with the given grpc server.
func (s *State) Register(grpcServer *grpc.Server) {
	pb.RegisterProducerAPIServer(grpcServer, &server{state: s})
}

// Mempool implements pb.ProducerAPIServer.
func (s *server) Mempool(stream grpc.ClientStreamingServer[pb.Transaction, pb.TransactionResp]) error {
	ctx := stream.Context()
	for {
		tx, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&pb.TransactionResp{})
		}
		if err != nil {
			return err
		}
		if tx == nil {
			continue
		}
		metrics.IncrCounter(1, metrics.NumTxsReceived)
		if err := s.state.PushToMempool(ctx, tx); err != nil {
			return err
		}
	}
}
