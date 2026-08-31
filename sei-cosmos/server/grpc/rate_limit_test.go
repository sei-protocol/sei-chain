package grpc

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/ratelimiter"
)

func cfg(rps float64, burst int, cidrs ...string) ratelimiter.Config {
	return ratelimiter.Config{RPS: rps, Burst: burst, TrustedProxyCIDRs: cidrs}
}

func mustNewRegistry(t *testing.T, c ratelimiter.Config) *ratelimiter.Registry {
	t.Helper()
	r, err := ratelimiter.New(c)
	require.NoError(t, err)
	return r
}

type mockAddr string

func (a mockAddr) Network() string { return "tcp" }
func (a mockAddr) String() string  { return string(a) }

func grpcCtx(ctx context.Context, peerAddr string, xff ...string) context.Context {
	ctx = peer.NewContext(ctx, &peer.Peer{Addr: mockAddr(peerAddr)})
	if len(xff) > 0 {
		md := metadata.MD{"x-forwarded-for": xff}
		ctx = metadata.NewIncomingContext(ctx, md)
	}
	return ctx
}

func TestUnaryRateLimitInterceptor_AllowThenReject(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 1))
	ic := UnaryRateLimitInterceptor(reg)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.bank.v1beta1.Query/Balance"}
	ctx := grpcCtx(t.Context(), "10.0.0.1:9000")

	_, err := ic(ctx, nil, info, handler)
	require.NoError(t, err)

	_, err = ic(ctx, nil, info, handler)
	require.Error(t, err)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestUnaryRateLimitInterceptor_PerIPIsolation(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 1))
	ic := UnaryRateLimitInterceptor(reg)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.bank.v1beta1.Query/Balance"}

	_, err := ic(grpcCtx(t.Context(), "1.1.1.1:9000"), nil, info, handler)
	require.NoError(t, err)
	_, err = ic(grpcCtx(t.Context(), "1.1.1.1:9000"), nil, info, handler)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))

	_, err = ic(grpcCtx(t.Context(), "2.2.2.2:9000"), nil, info, handler)
	require.NoError(t, err)
}

func TestUnaryRateLimitInterceptor_TrustedProxyXFF(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 1, "10.0.0.0/8"))
	ic := UnaryRateLimitInterceptor(reg)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.bank.v1beta1.Query/Balance"}
	ctx := grpcCtx(t.Context(), "10.0.0.2:9000", "203.0.113.5, 10.0.0.2")

	_, err := ic(ctx, nil, info, handler)
	require.NoError(t, err)
	_, err = ic(ctx, nil, info, handler)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

type mockServerStream struct {
	grpc.ServerStream
	ctx     context.Context
	recvErr error
}

func (m mockServerStream) Context() context.Context  { return m.ctx }
func (m mockServerStream) RecvMsg(interface{}) error { return m.recvErr }

func TestStreamRateLimitInterceptor_AllowThenReject(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 1))
	ic := StreamRateLimitInterceptor(reg)
	handler := func(srv interface{}, stream grpc.ServerStream) error {
		return nil
	}
	info := &grpc.StreamServerInfo{FullMethod: "/cosmos.bank.v1beta1.Query/Balance"}
	stream := mockServerStream{ctx: grpcCtx(t.Context(), "10.0.0.1:9000")}

	require.NoError(t, ic(nil, stream, info, handler))

	stream = mockServerStream{ctx: grpcCtx(t.Context(), "10.0.0.1:9000")}
	err := ic(nil, stream, info, handler)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestUnaryRateLimitInterceptor_ZeroRPSBypassesBucket(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0, 10))
	ic := UnaryRateLimitInterceptor(reg)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.bank.v1beta1.Query/Balance"}
	ctx := grpcCtx(t.Context(), "10.0.0.1:9000")

	for range 10 {
		_, err := ic(ctx, nil, info, handler)
		require.NoError(t, err)
	}
}

func TestUnaryRateLimitInterceptor_NoPeerStillRateLimited(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 1))
	ic := UnaryRateLimitInterceptor(reg)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.bank.v1beta1.Query/Balance"}
	ctx := t.Context()

	_, err := ic(ctx, nil, info, handler)
	require.NoError(t, err)
	_, err = ic(ctx, nil, info, handler)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestUnaryRateLimitInterceptor_RealTCPAddr(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 1))
	ic := UnaryRateLimitInterceptor(reg)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.bank.v1beta1.Query/Balance"}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	ctx := peer.NewContext(t.Context(), &peer.Peer{Addr: ln.Addr()})

	_, err = ic(ctx, nil, info, handler)
	require.NoError(t, err)
	_, err = ic(ctx, nil, info, handler)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestStreamRateLimitInterceptor_ChargesPerInboundMessage(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 2))
	ic := StreamRateLimitInterceptor(reg)
	handler := func(_ interface{}, stream grpc.ServerStream) error {
		require.NoError(t, stream.RecvMsg(nil))
		return stream.RecvMsg(nil)
	}
	info := &grpc.StreamServerInfo{FullMethod: "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"}
	stream := mockServerStream{ctx: grpcCtx(t.Context(), "10.0.0.1:9000")}

	err := ic(nil, stream, info, handler)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestStreamRateLimitInterceptor_RecvErrorIsNotCharged(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 1))
	ic := StreamRateLimitInterceptor(reg)
	handler := func(_ interface{}, stream grpc.ServerStream) error {
		return stream.RecvMsg(nil)
	}
	info := &grpc.StreamServerInfo{FullMethod: "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"}
	stream := mockServerStream{ctx: grpcCtx(t.Context(), "10.0.0.1:9000"), recvErr: io.EOF}

	require.Equal(t, io.EOF, ic(nil, stream, info, handler))
}
