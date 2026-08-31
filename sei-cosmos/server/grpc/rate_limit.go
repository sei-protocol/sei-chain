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
// token-bucket rate limiting to client streams. registry must be non-nil.
//
// A stream costs one token to establish and one more per inbound message, so an
// admitted stream cannot serve unbounded work.
func StreamRateLimitInterceptor(registry *ratelimiter.Registry) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		ip := registry.IPFromGRPCContext(ctx)
		if !registry.Allow(ctx, ip, ratelimiter.PlaneGRPC, info.FullMethod) {
			return errRateLimited
		}
		return handler(srv, rateLimitedServerStream{
			ServerStream: ss,
			registry:     registry,
			ip:           ip,
			method:       info.FullMethod,
		})
	}
}

// rateLimitedServerStream charges the per-IP bucket for each message the client
// sends on an admitted stream.
type rateLimitedServerStream struct {
	grpc.ServerStream

	registry *ratelimiter.Registry
	ip       string
	method   string
}

func (s rateLimitedServerStream) RecvMsg(m interface{}) error {
	// Charge after the read so a blocked stream holds no token and EOF is free.
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}
	if !s.registry.Allow(s.Context(), s.ip, ratelimiter.PlaneGRPC, s.method) {
		return errRateLimited
	}
	return nil
}
