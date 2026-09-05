package grpc

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"

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

func TestRateLimitTapHandle_AllowThenReject(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 1))
	tapHandle := RateLimitTapHandle(reg)
	info := &tap.Info{FullMethodName: "/cosmos.bank.v1beta1.Query/Balance"}
	ctx := grpcCtx(t.Context(), "10.0.0.1:9000")

	admittedCtx, err := tapHandle(ctx, info)
	require.NoError(t, err)
	ip, admitted := admittedIP(admittedCtx)
	require.True(t, admitted)
	require.Equal(t, "10.0.0.1", ip)

	_, err = tapHandle(ctx, info)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestRateLimitTapHandle_TrustedProxyXFF(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 1, "10.0.0.0/8"))
	tapHandle := RateLimitTapHandle(reg)
	info := &tap.Info{FullMethodName: "/cosmos.bank.v1beta1.Query/Balance"}
	ctx := grpcCtx(t.Context(), "10.0.0.2:9000", "203.0.113.5, 10.0.0.2")

	admittedCtx, err := tapHandle(ctx, info)
	require.NoError(t, err)
	ip, _ := admittedIP(admittedCtx)
	require.Equal(t, "203.0.113.5", ip)

	_, err = tapHandle(ctx, info)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

// TestUnaryRateLimitInterceptor_TapAdmissionIsNotChargedTwice pins that stacking
// the tap handler and the interceptor costs one token per RPC: with a burst of
// 1, the interceptor must not spend the token the tap handler left behind.
func TestUnaryRateLimitInterceptor_TapAdmissionIsNotChargedTwice(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 2))
	tapHandle := RateLimitTapHandle(reg)
	ic := UnaryRateLimitInterceptor(reg)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/cosmos.bank.v1beta1.Query/Balance"}
	ctx := grpcCtx(t.Context(), "10.0.0.1:9000")

	for range 2 {
		admittedCtx, err := tapHandle(ctx, &tap.Info{FullMethodName: info.FullMethod})
		require.NoError(t, err)
		_, err = ic(admittedCtx, nil, info, handler)
		require.NoError(t, err)
	}

	_, err := tapHandle(ctx, &tap.Info{FullMethodName: info.FullMethod})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

// TestStreamRateLimitInterceptor_TapAdmissionSkipsEstablishmentCharge pins that
// a tapped stream pays for establishment once: a burst of 2 covers the tap
// admission and one message, and the second message is rejected.
func TestStreamRateLimitInterceptor_TapAdmissionSkipsEstablishmentCharge(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 2))
	tapHandle := RateLimitTapHandle(reg)
	ic := StreamRateLimitInterceptor(reg)
	info := &grpc.StreamServerInfo{FullMethod: "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"}

	admittedCtx, err := tapHandle(grpcCtx(t.Context(), "10.0.0.1:9000"), &tap.Info{FullMethodName: info.FullMethod})
	require.NoError(t, err)

	handler := func(_ interface{}, stream grpc.ServerStream) error {
		require.NoError(t, stream.RecvMsg(nil))
		return stream.RecvMsg(nil)
	}
	err = ic(nil, mockServerStream{ctx: admittedCtx}, info, handler)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestRateLimitHTTPMiddleware_AllowThenReject(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 1))
	var served int
	handler := RateLimitHTTPMiddleware(reg, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		served++
	}))

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/testdata.Query/Echo", nil)
		r.RemoteAddr = "10.0.0.1:9000"
		return r
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, served)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, 1, served, "rejected request must not reach the wrapped server")
}

// TestRateLimitHTTPMiddleware_RecordsAdmissionOnRequestContext pins the handoff
// to the interceptors: the request context becomes the stream context, so the
// admission recorded here is what stops the RPC being charged twice.
func TestRateLimitHTTPMiddleware_RecordsAdmissionOnRequestContext(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 1))
	var got string
	var admitted bool
	handler := RateLimitHTTPMiddleware(reg, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, admitted = admittedIP(r.Context())
	}))

	r := httptest.NewRequest(http.MethodPost, "/testdata.Query/Echo", nil)
	r.RemoteAddr = "10.0.0.1:9000"
	handler.ServeHTTP(httptest.NewRecorder(), r)

	require.True(t, admitted)
	require.Equal(t, "10.0.0.1", got)
}

// TestRateLimitHTTPMiddleware_PreflightIsNotCharged keeps a browser's CORS
// preflight from costing the same token as the call it precedes.
func TestRateLimitHTTPMiddleware_PreflightIsNotCharged(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 1))
	handler := RateLimitHTTPMiddleware(reg, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	for range 3 {
		r := httptest.NewRequest(http.MethodOptions, "/testdata.Query/Echo", nil)
		r.RemoteAddr = "10.0.0.1:9000"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		require.Equal(t, http.StatusOK, rec.Code)
	}

	r := httptest.NewRequest(http.MethodPost, "/testdata.Query/Echo", nil)
	r.RemoteAddr = "10.0.0.1:9000"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRateLimitHTTPMiddleware_TrustedProxyXFF(t *testing.T) {
	reg := mustNewRegistry(t, cfg(0.001, 1, "10.0.0.0/8"))
	handler := RateLimitHTTPMiddleware(reg, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/testdata.Query/Echo", nil)
		r.RemoteAddr = "10.0.0.2:9000"
		r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.2")
		return r
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	require.Equal(t, http.StatusTooManyRequests, rec.Code)

	// A different client behind the same proxy keeps its own bucket.
	r := req()
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.2")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	require.Equal(t, http.StatusOK, rec.Code)
}
