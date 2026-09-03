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

// rateLimitServerOptions returns the tap-handler and interceptor options that
// apply per-IP admission to the gRPC plane. It returns nil when cfg leaves
// rate limiting disabled.
func rateLimitServerOptions(cfg config.GRPCConfig) ([]grpc.ServerOption, *ratelimiter.Registry, error) {
	if !cfg.RateLimitingEnabled {
		return nil, nil, nil
	}
	registry, err := ratelimiter.New(cfg.RateLimiterConfig())
	if err != nil {
		return nil, nil, fmt.Errorf("grpc rate limiter: %w", err)
	}
	// A zeroed bucket makes Allow admit unconditionally, so admission installs and
	// throttles nothing. The CometBFT RPC plane logs the same combination; without
	// this the gRPC plane would fail open silently.
	if cfg.IPRateLimitRPS <= 0 || cfg.IPRateLimitBurst <= 0 {
		logger.Info(
			"gRPC rate-limit admission is enabled but the token bucket is disabled "+
				"(ip-rate-limit-rps and/or ip-rate-limit-burst <= 0); ResourceExhausted throttling will not occur",
			"ip-rate-limit-rps", cfg.IPRateLimitRPS,
			"ip-rate-limit-burst", cfg.IPRateLimitBurst,
		)
	}
	opts := []grpc.ServerOption{
		// Admission happens before the request is decoded: the tap handler
		// covers native gRPC, and RateLimitHTTPMiddleware covers gRPC-Web,
		// which reaches this server through ServeHTTP. The interceptors charge
		// what neither can see, each message on an established stream, and
		// admit any RPC that arrived uncharged.
		grpc.InTapHandle(RateLimitTapHandle(registry)),
		grpc.ChainUnaryInterceptor(UnaryRateLimitInterceptor(registry)),
		grpc.ChainStreamInterceptor(StreamRateLimitInterceptor(registry)),
	}
	// The tap handler takes a per-IP concurrency slot alongside the token, and
	// the stats handler is what gives it back, on every path a stream can end.
	// Registering one at all makes grpc-go build and dispatch a stats event for
	// every phase of every RPC, so with the cap off it is left out rather than
	// left inert.
	if cfg.MaxInFlightPerIP > 0 {
		opts = append(opts, grpc.StatsHandler(InFlightStatsHandler(registry)))
	}
	return opts, registry, nil
}

// clampToMaxInt returns n as an int, saturating rather than wrapping.
//
// The connection limits are configured as unsigned and consumed as signed, and a
// value above the signed maximum would otherwise wrap to a negative one, which
// both listeners read as "no limit" — the opposite of what an operator writing a
// very large number asked for.
func clampToMaxInt(n uint) int {
	if n > math.MaxInt {
		return math.MaxInt
	}
	return int(n) //nolint:gosec // G115: clamped to math.MaxInt above
}

// registeredMethods returns the "service/Method" name of every method served by
// srv.
func registeredMethods(srv *grpc.Server) []string {
	info := srv.GetServiceInfo()
	total := 0
	for _, svcInfo := range info {
		total += len(svcInfo.Methods)
	}

	methods := make([]string, 0, total)
	for service, svcInfo := range info {
		for _, m := range svcInfo.Methods {
			methods = append(methods, service+"/"+m.Name)
		}
	}
	return methods
}

// StartGRPCServer starts a gRPC server on the address given by cfg. It returns
// the rate-limit registry the server admits against, or nil when cfg leaves rate
// limiting disabled, so the gRPC-Web server can share the same per-IP buckets.
func StartGRPCServer(clientCtx client.Context, app types.Application, cfg config.GRPCConfig) (*grpc.Server, *ratelimiter.Registry, error) {
	maxRecvMsgSize := cfg.MaxRecvMsgSize
	if maxRecvMsgSize <= 0 {
		maxRecvMsgSize = config.DefaultGRPCMaxRecvMsgSize
	}

	rateLimitOpts, rateLimitRegistry, err := rateLimitServerOptions(cfg)
	if err != nil {
		return nil, nil, err
	}

	serverOpts := append([]grpc.ServerOption{
		grpc.MaxConcurrentStreams(100),
		// MaxRecvMsgSize bounds the memory an admitted request may allocate,
		// preventing an oversized request from exhausting memory.
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
		return nil, nil, err
	}
	// Reflection allows external clients to see what services and methods
	// the gRPC server exposes.
	gogoreflection.Register(grpcSrv)
	if rateLimitRegistry != nil {
		rateLimitRegistry.SetKnownGRPCMethods(registeredMethods(grpcSrv))
	}

	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return nil, nil, err
	}
	// The per-IP cap wraps the raw listener and the global cap wraps that, so a
	// connection one address is refused never occupies a slot in the global
	// budget it would otherwise be able to hold whole.
	listener = ratelimiter.ConnLimitListener(listener, ratelimiter.PlaneGRPC, clampToMaxInt(cfg.MaxConnectionsPerIP))
	if cfg.MaxOpenConnections > 0 {
		listener = netutil.LimitListener(listener, clampToMaxInt(cfg.MaxOpenConnections))
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
		return nil, nil, err
	case <-time.After(types.ServerStartTime): // assume server started successfully
		return grpcSrv, rateLimitRegistry, nil
	}
}
