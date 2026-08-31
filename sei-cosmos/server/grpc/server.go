package grpc

import (
	"fmt"
	"math"
	"net"
	"time"

	"github.com/sei-protocol/seilog"
	"golang.org/x/net/netutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/sei-protocol/sei-chain/ratelimiter"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/grpc/gogoreflection"
	reflection "github.com/sei-protocol/sei-chain/sei-cosmos/server/grpc/reflection/v2alpha1"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

var logger = seilog.NewLogger("cosmos", "server", "grpc")

// rateLimitServerOptions returns the unary and stream interceptor options that
// apply per-IP admission to the gRPC plane. It returns nil when cfg leaves
// rate limiting disabled.
func rateLimitServerOptions(cfg config.GRPCConfig) ([]grpc.ServerOption, error) {
	if !cfg.RateLimitingEnabled {
		return nil, nil
	}
	registry, err := ratelimiter.New(cfg.RateLimiterConfig())
	if err != nil {
		return nil, fmt.Errorf("grpc rate limiter: %w", err)
	}
	// A zeroed bucket makes Allow admit unconditionally, so the interceptors
	// install and throttle nothing. The CometBFT RPC plane logs the same
	// combination; without this the gRPC plane would fail open silently.
	if cfg.IPRateLimitRPS <= 0 || cfg.IPRateLimitBurst <= 0 {
		logger.Info(
			"gRPC rate-limit admission is enabled but the token bucket is disabled "+
				"(ip-rate-limit-rps and/or ip-rate-limit-burst <= 0); ResourceExhausted throttling will not occur",
			"ip-rate-limit-rps", cfg.IPRateLimitRPS,
			"ip-rate-limit-burst", cfg.IPRateLimitBurst,
		)
	}
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(UnaryRateLimitInterceptor(registry)),
		grpc.ChainStreamInterceptor(StreamRateLimitInterceptor(registry)),
	}, nil
}

// StartGRPCServer starts a gRPC server on the address given by cfg.
func StartGRPCServer(clientCtx client.Context, app types.Application, cfg config.GRPCConfig) (*grpc.Server, error) {
	maxRecvMsgSize := cfg.MaxRecvMsgSize
	if maxRecvMsgSize <= 0 {
		maxRecvMsgSize = config.DefaultGRPCMaxRecvMsgSize
	}

	rateLimitOpts, err := rateLimitServerOptions(cfg)
	if err != nil {
		return nil, err
	}

	serverOpts := append([]grpc.ServerOption{
		grpc.MaxConcurrentStreams(100),
		// MaxRecvMsgSize bounds per-request memory allocation before the rate
		// limiter fires, preventing an oversized request from exhausting memory.
		grpc.MaxRecvMsgSize(maxRecvMsgSize),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     cfg.MaxConnectionIdle,
			MaxConnectionAge:      cfg.MaxConnectionAge,
			MaxConnectionAgeGrace: cfg.MaxConnectionAgeGrace,
			Time:                  cfg.KeepaliveTime,
			Timeout:               cfg.KeepaliveTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             cfg.KeepaliveMinTime,
			PermitWithoutStream: cfg.KeepalivePermitWithoutStream,
		}),
	}, rateLimitOpts...)

	grpcSrv := grpc.NewServer(serverOpts...)
	app.RegisterGRPCServer(grpcSrv)
	// reflection allows consumers to build dynamic clients that can write
	// to any cosmos-sdk application without relying on application packages at compile time
	err = reflection.Register(grpcSrv, reflection.Config{
		SigningModes: func() map[string]int32 {
			modes := make(map[string]int32, len(clientCtx.TxConfig.SignModeHandler().Modes()))
			for _, m := range clientCtx.TxConfig.SignModeHandler().Modes() {
				modes[m.String()] = (int32)(m)
			}
			return modes
		}(),
		ChainID:           clientCtx.ChainID,
		SdkConfig:         sdk.GetConfig(),
		InterfaceRegistry: clientCtx.InterfaceRegistry,
	})
	if err != nil {
		return nil, err
	}
	// Reflection allows external clients to see what services and methods
	// the gRPC server exposes.
	gogoreflection.Register(grpcSrv)
	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return nil, err
	}
	if cfg.MaxOpenConnections > 0 {
		maxConn := cfg.MaxOpenConnections
		if maxConn > math.MaxInt {
			maxConn = math.MaxInt
		}
		listener = netutil.LimitListener(listener, int(maxConn)) //nolint:gosec // G115: clamped to math.MaxInt above
	}

	errCh := make(chan error)
	go func() {
		err = grpcSrv.Serve(listener)
		if err != nil {
			errCh <- fmt.Errorf("failed to serve: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return nil, err
	case <-time.After(types.ServerStartTime): // assume server started successfully
		return grpcSrv, nil
	}
}
