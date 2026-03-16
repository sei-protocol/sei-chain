package admin

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/admin/types"
	"github.com/sei-protocol/seilog"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func TestStartServer_RejectsNonLoopback(t *testing.T) {
	tests := []struct {
		name    string
		address string
	}{
		{"public ipv4", "192.168.1.1:0"},
		{"all interfaces", "0.0.0.0:0"},
		{"hostname", "localhost:0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, err := StartServer(tt.address)
			if srv != nil {
				srv.GracefulStop()
			}
			require.Error(t, err, "non-loopback address %q should be rejected", tt.address)
		})
	}
}

func TestStartServer_AcceptsLoopback(t *testing.T) {
	srv, err := StartServer("127.0.0.1:0")
	require.NoError(t, err)
	srv.GracefulStop()
}

func TestStartServer_AcceptsIPv6Loopback(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 loopback not available")
	}
	ln.Close()

	srv, err := StartServer("[::1]:0")
	require.NoError(t, err)
	srv.GracefulStop()
}

func TestStartServer_GRPCServiceReachable(t *testing.T) {
	_ = seilog.NewLogger("srv-test-reachable")

	addr := freeLoopbackAddr(t)
	srv, err := StartServer(addr)
	require.NoError(t, err)
	defer srv.GracefulStop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close()) }()

	client := types.NewAdminServiceClient(conn)
	resp, err := client.ListLoggers(ctx, &types.ListLoggersRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Loggers)
}

func TestStartServer_SetLogLevelViaGRPC(t *testing.T) {
	_ = seilog.NewLogger("srv-test-set")

	addr := freeLoopbackAddr(t)
	srv, err := StartServer(addr)
	require.NoError(t, err)
	defer srv.GracefulStop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close()) }()

	client := types.NewAdminServiceClient(conn)

	setResp, err := client.SetLogLevel(ctx, &types.SetLogLevelRequest{
		Pattern: "srv-test-set",
		Level:   "error",
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), setResp.Affected)

	getResp, err := client.GetLogLevel(ctx, &types.GetLogLevelRequest{
		Logger: "srv-test-set",
	})
	require.NoError(t, err)
	require.Equal(t, "error", getResp.Level)
}

func TestStartServer_GracefulStop(t *testing.T) {
	srv, err := StartServer("127.0.0.1:0")
	require.NoError(t, err)
	srv.GracefulStop()
}

func TestStartServer_PortAlreadyInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { require.NoError(t, ln.Close()) }()

	_, err = StartServer(ln.Addr().String())
	require.Error(t, err, "should fail when port is already in use")
}
