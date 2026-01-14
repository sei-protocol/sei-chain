package producer

import (
	"errors"
	"io"

	"google.golang.org/grpc"

	"github.com/tendermint/tendermint/internal/autobahn/pkg/metrics"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
)

// Server implements protocol.ProducerAPIServer.
type server struct {
	protocol.UnimplementedProducerAPIServer
	state *State
}

// Register registers ProducerAPIServer with the given grpc server.
func (s *State) Register(grpcServer *grpc.Server) {
	protocol.RegisterProducerAPIServer(grpcServer, &server{state: s})
}

// Mempool implements protocol.ProducerAPIServer.
func (s *server) Mempool(stream grpc.ClientStreamingServer[protocol.Transaction, protocol.TransactionResp]) error {
	ctx := stream.Context()
	for {
		tx, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&protocol.TransactionResp{})
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
