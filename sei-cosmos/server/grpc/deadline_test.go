package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/ratelimiter"
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

func TestDeadlineStatusError(t *testing.T) {
	require.NoError(t, deadlineStatusError(nil))

	converted := deadlineStatusError(context.DeadlineExceeded)
	require.Equal(t, codes.DeadlineExceeded, status.Code(converted))

	already := status.Error(codes.DeadlineExceeded, "already a status")
	require.Equal(t, already, deadlineStatusError(already))

	other := errors.New("boom")
	require.Equal(t, other, deadlineStatusError(other))
}
