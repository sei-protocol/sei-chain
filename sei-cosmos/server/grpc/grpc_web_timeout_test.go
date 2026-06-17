package grpc_test

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	srvgrpc "github.com/sei-protocol/sei-chain/sei-cosmos/server/grpc"
)

// TestStartGRPCWebTimeouts verifies that StartGRPCWeb sets all HTTP timeout
// fields needed to prevent body-stall DoS attacks.
func TestStartGRPCWebTimeouts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	err = ln.Close()
	require.NoError(t, err)

	grpcSrv := grpc.NewServer()
	cfg := config.Config{
		GRPCWeb: config.GRPCWebConfig{
			Enable:  true,
			Address: addr,
		},
	}

	srv, err := srvgrpc.StartGRPCWeb(grpcSrv, cfg)
	require.NoError(t, err)
	require.NotNil(t, srv)
	defer func() {
		err = srv.Shutdown(t.Context()) //nolint:errcheck
		require.NoError(t, err)
	}()

	require.Equal(t, 10*time.Second, srv.ReadHeaderTimeout, "ReadHeaderTimeout")
	require.Equal(t, 30*time.Second, srv.ReadTimeout, "ReadTimeout")
	require.Equal(t, 2*time.Minute, srv.WriteTimeout, "WriteTimeout")
	require.Equal(t, 30*time.Second, srv.IdleTimeout, "IdleTimeout")
}
