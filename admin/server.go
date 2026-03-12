package admin

import (
	"fmt"
	"net"

	"github.com/sei-protocol/sei-chain/admin/types"
	"github.com/sei-protocol/seilog"
	"google.golang.org/grpc"
)

var logger = seilog.NewLogger("admin")

// StartServer creates and starts a dedicated admin gRPC server on the given
// loopback address. Returns the server so the caller can stop it on shutdown.
func StartServer(address string) (*grpc.Server, error) {
	if err := validateLoopback(address); err != nil {
		return nil, err
	}

	grpcSrv := grpc.NewServer()
	types.RegisterAdminServiceServer(grpcSrv, &service{})

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("admin server: failed to listen on %s: %w", address, err)
	}

	go func() {
		logger.Info("Admin gRPC server started", "address", listener.Addr())
		if err := grpcSrv.Serve(listener); err != nil {
			logger.Error("Admin gRPC server stopped erroneously", "err", err)
		}
	}()

	return grpcSrv, nil
}
