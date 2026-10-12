package baseapp

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

type contextQuery struct {
	testdata.QueryImpl
	ctx chan context.Context
}

func (q contextQuery) Echo(ctx context.Context, req *testdata.EchoRequest) (*testdata.EchoResponse, error) {
	q.ctx <- sdk.UnwrapSDKContext(ctx).Context()
	return q.QueryImpl.Echo(ctx, req)
}

// serveTestQuery registers the testdata Query service on app, exposes it on a real
// grpc.Server built with serverOpts, and returns a client dialled to it.
//
// The service is registered through RegisterGRPCServer so calls travel grpc-go's
// own dispatch path. An interceptor supplied through serverOpts reaches handlers
// only if that path is intact, which invoking the interceptor closure directly
// cannot show.
func serveTestQuery(t *testing.T, app *BaseApp, serverOpts ...grpc.ServerOption) testdata.QueryClient {
	return serveTestQueryWithHandler(t, app, testdata.QueryImpl{}, serverOpts...)
}

func serveTestQueryWithHandler(t *testing.T, app *BaseApp, handler testdata.QueryServer, serverOpts ...grpc.ServerOption) testdata.QueryClient {
	t.Helper()

	interfaceRegistry := types.NewInterfaceRegistry()
	testdata.RegisterInterfaces(interfaceRegistry)
	app.SetInterfaceRegistry(interfaceRegistry)
	testdata.RegisterQueryServer(app.GRPCQueryRouter(), handler)

	srv := grpc.NewServer(serverOpts...)
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

func TestRegisterGRPCServerAppliesServerInterceptor(t *testing.T) {
	var calls atomic.Int64
	spy := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		calls.Add(1)
		return handler(ctx, req)
	}

	client := serveTestQuery(t, setupBaseApp(t), grpc.ChainUnaryInterceptor(spy))

	res, err := client.Echo(t.Context(), &testdata.EchoRequest{Message: "hello"})
	require.NoError(t, err)
	require.Equal(t, "hello", res.Message)
	require.Equal(t, int64(1), calls.Load(), "server-level interceptor never reached the query handler")
}

func TestRegisterGRPCServerServerInterceptorCanRejectQuery(t *testing.T) {
	var handlerCalls atomic.Int64
	reject := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return nil, status.Error(codes.ResourceExhausted, "too many requests")
	}
	count := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		handlerCalls.Add(1)
		return handler(ctx, req)
	}

	client := serveTestQuery(t, setupBaseApp(t), grpc.ChainUnaryInterceptor(reject, count))

	_, err := client.Echo(t.Context(), &testdata.EchoRequest{Message: "hello"})
	require.Error(t, err)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Zero(t, handlerCalls.Load(), "rejected query still ran the rest of the chain")
}

func TestRegisterGRPCServerWithoutServerInterceptor(t *testing.T) {
	client := serveTestQuery(t, setupBaseApp(t))

	res, err := client.Echo(t.Context(), &testdata.EchoRequest{Message: "hello"})
	require.NoError(t, err)
	require.Equal(t, "hello", res.Message)
}

func TestRegisterGRPCServerThreadsServerContextIntoSDKContext(t *testing.T) {
	queryCtx := make(chan context.Context, 1)
	bound := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		return handler(ctx, req)
	}

	client := serveTestQueryWithHandler(t, setupBaseApp(t), contextQuery{ctx: queryCtx}, grpc.ChainUnaryInterceptor(bound))
	_, err := client.Echo(t.Context(), &testdata.EchoRequest{Message: "hello"})
	require.NoError(t, err)

	deadline, ok := (<-queryCtx).Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(time.Minute), deadline, time.Second)
}
