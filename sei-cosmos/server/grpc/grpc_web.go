package grpc

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"time"

	"github.com/improbable-eng/grpc-web/go/grpcweb"
	"golang.org/x/net/netutil"
	"google.golang.org/grpc"

	"github.com/sei-protocol/sei-chain/ratelimiter"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
)

// StartGRPCWeb starts a gRPC-Web server on the given address. registry is the
// rate-limit registry StartGRPCServer returned; when it is non-nil the two
// planes admit against the same per-IP buckets, and when it is nil gRPC-Web
// serves without per-IP admission.
func StartGRPCWeb(grpcSrv *grpc.Server, registry *ratelimiter.Registry, config config.Config) (*http.Server, error) {
	var options []grpcweb.Option
	if config.GRPCWeb.EnableUnsafeCORS {
		options = append(options,
			grpcweb.WithOriginFunc(func(origin string) bool {
				return true
			}),
		)
	}

	var handler http.Handler = grpcweb.WrapServer(grpcSrv, options...)
	if registry != nil {
		handler = RateLimitHTTPMiddleware(registry, handler)
	}
	grpcWebSrv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       30 * time.Second,
	}

	listener, err := net.Listen("tcp", config.GRPCWeb.Address)
	if err != nil {
		return nil, fmt.Errorf("[grpc-web] failed to listen on %s: %w", config.GRPCWeb.Address, err)
	}
	if config.GRPCWeb.MaxOpenConnections > 0 {
		maxConn := config.GRPCWeb.MaxOpenConnections
		if maxConn > math.MaxInt {
			maxConn = math.MaxInt
		}
		listener = netutil.LimitListener(listener, int(maxConn)) //nolint:gosec // G115: clamped to math.MaxInt above
	}

	errCh := make(chan error, 1)
	go func() {
		if err := grpcWebSrv.Serve(listener); err != nil {
			errCh <- fmt.Errorf("[grpc-web] failed to serve: %w", err)
		}
	}()

	select {
	case err = <-errCh:
		return nil, err
	case <-time.After(types.ServerStartTime): // assume server started successfully
		return grpcWebSrv, nil
	}
}
