package grpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/ratelimiter"
)

var errRateLimited = status.Error(codes.ResourceExhausted, "too many requests")

// UnaryRateLimitInterceptor returns a server interceptor that applies per-IP
// token-bucket rate limiting before the handler runs. registry must be non-nil.
func UnaryRateLimitInterceptor(registry *ratelimiter.Registry) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ip := registry.IPFromGRPCContext(ctx)
		if !registry.Allow(ctx, ip, ratelimiter.PlaneGRPC, info.FullMethod) {
			return nil, errRateLimited
		}
		return handler(ctx, req)
	}
}

// StreamRateLimitInterceptor returns a server interceptor that applies per-IP
// token-bucket rate limiting when a client stream is established. registry must
// be non-nil.
func StreamRateLimitInterceptor(registry *ratelimiter.Registry) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		ip := registry.IPFromGRPCContext(ctx)
		if !registry.Allow(ctx, ip, ratelimiter.PlaneGRPC, info.FullMethod) {
			return errRateLimited
		}
		return handler(srv, ss)
	}
}
