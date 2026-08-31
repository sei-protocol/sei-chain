package grpc

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/ratelimiter"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/grpc/gogoreflection"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// serveRateLimitedQuery exposes the testdata Query service behind registry on a
// real grpc.Server and returns a client dialled to it.
//
// Registration goes through BaseApp.RegisterGRPCServer, the same call
// StartGRPCServer makes, so the interceptor is admitted the way it is on a live
// node rather than by invoking its closure directly.
func serveRateLimitedQuery(t *testing.T, registry *ratelimiter.Registry) testdata.QueryClient {
	t.Helper()

	app := baseapp.NewBaseApp(t.Name(), dbm.NewMemDB(), nil, nil, &testutil.TestAppOpts{})
	app.MountStores(sdk.NewKVStoreKey("test"))
	require.NoError(t, app.LoadLatestVersion())

	interfaceRegistry := codectypes.NewInterfaceRegistry()
	testdata.RegisterInterfaces(interfaceRegistry)
	app.SetInterfaceRegistry(interfaceRegistry)
	testdata.RegisterQueryServer(app.GRPCQueryRouter(), testdata.QueryImpl{})

	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(UnaryRateLimitInterceptor(registry)))
	app.RegisterGRPCServer(srv)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.Dial(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return testdata.NewQueryClient(conn)
}

func TestUnaryRateLimitInterceptor_RegisteredQueryServiceAllowThenReject(t *testing.T) {
	client := serveRateLimitedQuery(t, mustNewRegistry(t, cfg(0.001, 1)))

	res, err := client.Echo(context.Background(), &testdata.EchoRequest{Message: "hello"})
	require.NoError(t, err)
	require.Equal(t, "hello", res.Message)

	_, err = client.Echo(context.Background(), &testdata.EchoRequest{Message: "hello"})
	require.Error(t, err)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestUnaryRateLimitInterceptor_RegisteredQueryServiceUnlimited(t *testing.T) {
	client := serveRateLimitedQuery(t, mustNewRegistry(t, cfg(1000, 1000)))

	for i := 0; i < 5; i++ {
		res, err := client.Echo(context.Background(), &testdata.EchoRequest{Message: "hello"})
		require.NoError(t, err)
		require.Equal(t, "hello", res.Message)
	}
}

// TestStreamRateLimitInterceptor_ReflectionChargesPerMessage exercises the only
// streaming surface on :9090 over a real server: a burst of 2 covers the stream
// establishment and one lookup, so the second lookup is rejected.
func TestStreamRateLimitInterceptor_ReflectionChargesPerMessage(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 2))
	srv := grpc.NewServer(grpc.ChainStreamInterceptor(StreamRateLimitInterceptor(reg)))
	gogoreflection.Register(srv)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.Dial(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	stream, err := rpb.NewServerReflectionClient(conn).ServerReflectionInfo(context.Background())
	require.NoError(t, err)

	listServices := &rpb.ServerReflectionRequest{
		MessageRequest: &rpb.ServerReflectionRequest_ListServices{},
	}
	require.NoError(t, stream.Send(listServices))
	_, err = stream.Recv()
	require.NoError(t, err)

	require.NoError(t, stream.Send(listServices))
	_, err = stream.Recv()
	require.Error(t, err)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}
