package grpc

import (
	"fmt"
	"math"
	"net"
	"time"

	"golang.org/x/net/netutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/grpc/gogoreflection"
	reflection "github.com/sei-protocol/sei-chain/sei-cosmos/server/grpc/reflection/v2alpha1"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// StartGRPCServer starts a gRPC server on the address given by cfg.
func StartGRPCServer(clientCtx client.Context, app types.Application, cfg config.GRPCConfig) (*grpc.Server, error) {
	maxRecvMsgSize := cfg.MaxRecvMsgSize
	if maxRecvMsgSize <= 0 {
		maxRecvMsgSize = config.DefaultGRPCMaxRecvMsgSize
	}

	grpcSrv := grpc.NewServer(
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
	)
	app.RegisterGRPCServer(grpcSrv)
	// reflection allows consumers to build dynamic clients that can write
	// to any cosmos-sdk application without relying on application packages at compile time
	err := reflection.Register(grpcSrv, reflection.Config{
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
