package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/ratelimiter"
)

const deadlinePlane = "grpc"

type deadlineCancelKey struct{}

type deadlineStatsHandler struct {
	enforcer *ratelimiter.DeadlineEnforcer
}

func (h deadlineStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	ctx, cancel := h.enforcer.WithDeadline(ctx, info.FullMethodName)
	return context.WithValue(ctx, deadlineCancelKey{}, cancel)
}

func (deadlineStatsHandler) HandleRPC(ctx context.Context, rpcStats stats.RPCStats) {
	if _, ok := rpcStats.(*stats.End); !ok {
		return
	}
	if cancel, ok := ctx.Value(deadlineCancelKey{}).(context.CancelFunc); ok {
		cancel()
	}
}

func (deadlineStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (deadlineStatsHandler) HandleConn(context.Context, stats.ConnStats) {}

func withHandlerDeadline(
	ctx context.Context,
	enforcer *ratelimiter.DeadlineEnforcer,
	method string,
) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Value(deadlineCancelKey{}).(context.CancelFunc); ok {
		return ctx, func() {}
	}
	return enforcer.WithDeadline(ctx, method)
}

func recordDeadlineExceeded(ctx context.Context, enforcer *ratelimiter.DeadlineEnforcer, method string, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded {
		enforcer.RecordExceeded(ctx, deadlinePlane, method)
	}
}

// deadlineStatusError normalizes a bare context.DeadlineExceeded returned by a
// handler into a DeadlineExceeded gRPC status.
func deadlineStatusError(err error) error {
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	return status.Error(codes.DeadlineExceeded, err.Error())
}

// unaryDeadlineInterceptor returns a server interceptor that bounds ctx by
// enforcer's effective deadline for the method before invoking handler.
// enforcer must be non-nil.
func unaryDeadlineInterceptor(enforcer *ratelimiter.DeadlineEnforcer) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, cancel := withHandlerDeadline(ctx, enforcer, info.FullMethod)
		defer cancel()
		resp, err := handler(ctx, req)
		recordDeadlineExceeded(ctx, enforcer, info.FullMethod, err)
		return resp, deadlineStatusError(err)
	}
}

// streamDeadlineInterceptor returns a server interceptor that bounds the
// stream's context by enforcer's effective deadline for the method before
// invoking handler. enforcer must be non-nil.
func streamDeadlineInterceptor(enforcer *ratelimiter.DeadlineEnforcer) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, cancel := withHandlerDeadline(ss.Context(), enforcer, info.FullMethod)
		defer cancel()
		err := handler(srv, deadlineServerStream{ServerStream: ss, ctx: ctx})
		recordDeadlineExceeded(ctx, enforcer, info.FullMethod, err)
		return deadlineStatusError(err)
	}
}

// deadlineServerStream overrides ServerStream.Context so a stream handler
// observes the bounded deadline rather than the stream's original context.
type deadlineServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s deadlineServerStream) Context() context.Context { return s.ctx }

func (s deadlineServerStream) RecvMsg(m any) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	return s.ServerStream.RecvMsg(m)
}

func (s deadlineServerStream) SendMsg(m any) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	return s.ServerStream.SendMsg(m)
}
