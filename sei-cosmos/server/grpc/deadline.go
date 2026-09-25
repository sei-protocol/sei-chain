package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/ratelimiter"
)

const deadlinePlane = "grpc"

func recordDeadlineExceeded(ctx context.Context, enforcer *ratelimiter.DeadlineEnforcer, method string, err error) {
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
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

// UnaryDeadlineInterceptor returns a server interceptor that bounds ctx by
// enforcer's effective deadline for the method before invoking handler.
// enforcer must be non-nil.
func UnaryDeadlineInterceptor(enforcer *ratelimiter.DeadlineEnforcer) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, cancel := enforcer.WithDeadline(ctx, info.FullMethod)
		defer cancel()
		resp, err := handler(ctx, req)
		recordDeadlineExceeded(ctx, enforcer, info.FullMethod, err)
		return resp, deadlineStatusError(err)
	}
}

// StreamDeadlineInterceptor returns a server interceptor that bounds the
// stream's context by enforcer's effective deadline for the method before
// invoking handler. enforcer must be non-nil.
func StreamDeadlineInterceptor(enforcer *ratelimiter.DeadlineEnforcer) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, cancel := enforcer.WithDeadline(ss.Context(), info.FullMethod)
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
