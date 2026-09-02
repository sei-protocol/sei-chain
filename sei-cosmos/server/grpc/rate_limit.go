package grpc

import (
	"context"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"

	"github.com/sei-protocol/sei-chain/ratelimiter"
)

var errRateLimited = status.Error(codes.ResourceExhausted, "too many requests")

// admittedIPKey addresses the client IP recorded when the tap handler charges an RPC.
type admittedIPKey struct{}

// admittedIP returns the client IP the tap handler charged for this RPC and
// whether the tap handler ran at all.
func admittedIP(ctx context.Context) (string, bool) {
	ip, ok := ctx.Value(admittedIPKey{}).(string)
	return ip, ok
}

// RateLimitTapHandle returns a tap handler that charges the per-IP bucket when a
// stream's headers arrive. registry must be non-nil.
//
// This is the earliest admission point gRPC offers: it runs before the request
// message is read off the wire and unmarshalled, so a throttled caller cannot
// spend the server's decoder. Interceptors run after that decode and so cannot.
// It executes on the connection's I/O goroutine, which is why it does nothing
// beyond the bucket check.
//
// Only the HTTP/2 transport calls it. gRPC-Web reaches the same server through
// ServeHTTP, where RateLimitHTTPMiddleware is the admission point instead.
func RateLimitTapHandle(registry *ratelimiter.Registry) tap.ServerInHandle {
	return func(ctx context.Context, info *tap.Info) (context.Context, error) {
		ip := registry.IPFromGRPCContext(ctx)
		if !registry.Allow(ctx, ip, ratelimiter.PlaneGRPC, info.FullMethodName) {
			return ctx, errRateLimited
		}
		return context.WithValue(ctx, admittedIPKey{}, ip), nil
	}
}

// RateLimitHTTPMiddleware returns a handler that charges the per-IP bucket
// before next sees the request. registry must be non-nil.
//
// This is the admission point for gRPC-Web: it reaches the gRPC server through
// ServeHTTP rather than the HTTP/2 transport, so the tap handler never runs and
// the interceptors would otherwise admit it only after the body was read and
// decoded. The admission is recorded on the request context, which becomes the
// stream context, so the interceptors do not charge the RPC a second time.
func RateLimitHTTPMiddleware(registry *ratelimiter.Registry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A CORS preflight carries no RPC payload, and charging it would cost a
		// browser two tokens per call.
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		ip := registry.IPFromHTTPRequest(r)
		if !registry.Allow(r.Context(), ip, ratelimiter.PlaneGRPC, r.URL.Path) {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), admittedIPKey{}, ip)))
	})
}

// UnaryRateLimitInterceptor returns a server interceptor that applies per-IP
// token-bucket rate limiting before the handler runs. registry must be non-nil.
//
// An RPC the tap handler or RateLimitHTTPMiddleware already charged passes
// through untouched, so a request costs one token however it reached the server.
func UnaryRateLimitInterceptor(registry *ratelimiter.Registry) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if _, admitted := admittedIP(ctx); !admitted {
			ip := registry.IPFromGRPCContext(ctx)
			if !registry.Allow(ctx, ip, ratelimiter.PlaneGRPC, info.FullMethod) {
				return nil, errRateLimited
			}
		}
		return handler(ctx, req)
	}
}

// StreamRateLimitInterceptor returns a server interceptor that applies per-IP
// token-bucket rate limiting to client streams. registry must be non-nil.
//
// A stream costs one token to establish and one more per inbound message, so an
// admitted stream cannot serve unbounded work. The establishment token is
// charged here only when neither the tap handler nor RateLimitHTTPMiddleware
// already charged it.
func StreamRateLimitInterceptor(registry *ratelimiter.Registry) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		ip, admitted := admittedIP(ctx)
		if !admitted {
			ip = registry.IPFromGRPCContext(ctx)
			if !registry.Allow(ctx, ip, ratelimiter.PlaneGRPC, info.FullMethod) {
				return errRateLimited
			}
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
	// Charge after the read so a caller is only ever throttled for a message it
	// actually sent: charging before would kill an idle stream whose client is
	// waiting rather than sending, and would spend a token on EOF. The cost is
	// that one message per stream may be decoded past the budget, bounded by
	// MaxRecvMsgSize.
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}
	if !s.registry.Allow(s.Context(), s.ip, ratelimiter.PlaneGRPC, s.method) {
		return errRateLimited
	}
	return nil
}
