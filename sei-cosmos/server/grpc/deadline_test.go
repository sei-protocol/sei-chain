package grpc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/ratelimiter"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/grpc/gogoreflection"
)

func newEnforcer(d time.Duration) *ratelimiter.DeadlineEnforcer {
	return ratelimiter.NewDeadlineEnforcer(ratelimiter.DeadlineConfig{Default: d})
}

func TestUnaryDeadlineInterceptor_AppliesDefaultDeadline(t *testing.T) {
	enforcer := newEnforcer(10 * time.Millisecond)
	ic := UnaryDeadlineInterceptor(enforcer)
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.tx.v1beta1.Service/Simulate"}

	handler := func(ctx context.Context, req any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	_, err := ic(t.Context(), nil, info, handler)
	require.Error(t, err)
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
}

func TestUnaryDeadlineInterceptor_ClientDeadlineShorterThanDefaultWins(t *testing.T) {
	// A large default; the client's own shorter deadline must be what fires.
	enforcer := newEnforcer(time.Hour)
	ic := UnaryDeadlineInterceptor(enforcer)
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.tx.v1beta1.Service/Simulate"}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	handler := func(ctx context.Context, req any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	start := time.Now()
	_, err := ic(ctx, nil, info, handler)
	require.Less(t, time.Since(start), time.Second)
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
}

func TestUnaryDeadlineInterceptor_HandlerSucceedsWithinDeadline(t *testing.T) {
	enforcer := newEnforcer(time.Minute)
	ic := UnaryDeadlineInterceptor(enforcer)
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.tx.v1beta1.Service/Simulate"}

	handler := func(ctx context.Context, req any) (any, error) {
		return "ok", nil
	}

	resp, err := ic(t.Context(), nil, info, handler)
	require.NoError(t, err)
	require.Equal(t, "ok", resp)
}

func TestUnaryDeadlineInterceptor_NonDeadlineErrorPassesThroughUnchanged(t *testing.T) {
	enforcer := newEnforcer(time.Minute)
	ic := UnaryDeadlineInterceptor(enforcer)
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.tx.v1beta1.Service/Simulate"}

	wantErr := status.Error(codes.InvalidArgument, "bad request")
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, wantErr
	}

	_, err := ic(t.Context(), nil, info, handler)
	require.Equal(t, wantErr, err)
}

func TestUnaryDeadlineInterceptor_StatusDeadlineErrorRecordsMetric(t *testing.T) {
	reader := collectRejectionMetrics(t)
	const metricName = "rpc_deadline_exceeded_total"
	before := rejectionCounts(t, reader, metricName)["other"]

	enforcer := newEnforcer(time.Minute)
	ic := UnaryDeadlineInterceptor(enforcer)
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.tx.v1beta1.Service/Simulate"}

	handler := func(ctx context.Context, req any) (any, error) {
		return nil, status.Error(codes.DeadlineExceeded, "simulation deadline exceeded")
	}

	_, err := ic(t.Context(), nil, info, handler)
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
	require.Equal(t, before+1, rejectionCounts(t, reader, metricName)["other"])
}

type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s fakeServerStream) Context() context.Context { return s.ctx }

func TestStreamDeadlineInterceptor_HandlerObservesBoundedContext(t *testing.T) {
	enforcer := newEnforcer(10 * time.Millisecond)
	ic := StreamDeadlineInterceptor(enforcer)
	info := &grpc.StreamServerInfo{FullMethod: "/cosmos.tx.v1beta1.Service/GetTxsEvent"}

	handler := func(srv any, stream grpc.ServerStream) error {
		<-stream.Context().Done()
		return stream.Context().Err()
	}

	err := ic(nil, fakeServerStream{ctx: t.Context()}, info, handler)
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
}

// blockingRecvStream simulates a handler parked in RecvMsg while the client sends nothing
type blockingRecvStream struct {
	grpc.ServerStream
	ctx  context.Context
	recv func() error
}

func (s blockingRecvStream) Context() context.Context { return s.ctx }

func (s blockingRecvStream) RecvMsg(any) error { return s.recv() }

func TestStreamDeadlineInterceptor_RecvMsgUnblocksOnDeadline(t *testing.T) {
	enforcer := newEnforcer(10 * time.Millisecond)
	ic := StreamDeadlineInterceptor(enforcer)
	info := &grpc.StreamServerInfo{FullMethod: "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"}

	recvEntered := make(chan struct{})
	blockRecv := make(chan struct{})

	underlying := blockingRecvStream{
		ctx: t.Context(),
		recv: func() error {
			close(recvEntered)
			<-blockRecv
			return nil
		},
	}

	handler := func(_ any, stream grpc.ServerStream) error {
		return stream.RecvMsg(nil)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- ic(nil, underlying, info, handler)
	}()

	<-recvEntered
	start := time.Now()
	err := <-errCh
	require.Less(t, time.Since(start), time.Second)
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
}

func TestStreamDeadlineAfterRateLimit_RecvMsgUnblocksOnDeadline(t *testing.T) {
	reg := mustNewRegistry(t, cfg(1000, 1000))
	enforcer := newEnforcer(10 * time.Millisecond)
	rateIC := StreamRateLimitInterceptor(reg)
	deadlineIC := StreamDeadlineInterceptor(enforcer)
	info := &grpc.StreamServerInfo{FullMethod: "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"}

	recvEntered := make(chan struct{})
	blockRecv := make(chan struct{})

	underlying := blockingRecvStream{
		ctx: grpcCtx(t.Context(), "10.0.0.1:9000"),
		recv: func() error {
			close(recvEntered)
			<-blockRecv
			return nil
		},
	}

	handler := func(_ any, stream grpc.ServerStream) error {
		return stream.RecvMsg(nil)
	}

	errCh := make(chan error, 1)
	go func() {
		wrapped := func(srv any, stream grpc.ServerStream) error {
			return deadlineIC(srv, stream, info, handler)
		}
		errCh <- rateIC(nil, underlying, info, wrapped)
	}()

	<-recvEntered
	err := <-errCh
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
}

func TestStreamDeadlineInterceptor_IdleStreamOnWire(t *testing.T) {
	enforcer := newEnforcer(50 * time.Millisecond)
	srv := grpc.NewServer(grpc.ChainStreamInterceptor(StreamDeadlineInterceptor(enforcer)))
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
	t.Cleanup(func() { _ = stream.CloseSend() })

	start := time.Now()
	_, err = stream.Recv()
	require.Error(t, err)
	require.Less(t, time.Since(start), 2*time.Second)
	require.GreaterOrEqual(t, time.Since(start), 40*time.Millisecond)
}

func TestDeadlineStatusError(t *testing.T) {
	require.NoError(t, deadlineStatusError(nil))

	converted := deadlineStatusError(context.DeadlineExceeded)
	require.Equal(t, codes.DeadlineExceeded, status.Code(converted))

	already := status.Error(codes.DeadlineExceeded, "already a status")
	require.Equal(t, already, deadlineStatusError(already))

	other := errors.New("boom")
	require.Equal(t, other, deadlineStatusError(other))
}
