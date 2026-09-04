package grpc

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	gogogrpc "github.com/gogo/protobuf/grpc"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/ratelimiter"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	moduletestutil "github.com/sei-protocol/sei-chain/sei-cosmos/types/module/testutil"
)

// blockingQuery serves testdata.Query with an Echo that holds its slot until the
// test releases it or the client goes away.
//
// The concurrency cap is only observable while RPCs overlap, and an RPC overlaps
// another only for as long as its handler runs.
type blockingQuery struct {
	testdata.QueryImpl

	entered     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
}

func newBlockingQuery() *blockingQuery {
	return &blockingQuery{
		entered: make(chan struct{}, 64),
		release: make(chan struct{}),
	}
}

func (q *blockingQuery) Echo(ctx context.Context, req *testdata.EchoRequest) (*testdata.EchoResponse, error) {
	q.entered <- struct{}{}
	select {
	case <-q.release:
		return &testdata.EchoResponse{Message: req.Message}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// releaseAll lets every blocked handler return, now and in future.
func (q *blockingQuery) releaseAll() { q.releaseOnce.Do(func() { close(q.release) }) }

// waitEntered blocks until n Echo handlers are running.
func (q *blockingQuery) waitEntered(t *testing.T, n int) {
	t.Helper()
	for range n {
		select {
		case <-q.entered:
		case <-time.After(10 * time.Second):
			t.Fatal("an Echo handler never started")
		}
	}
}

// blockingQueryApp registers the blocking Query service directly on the gRPC
// server rather than on the BaseApp query router.
//
// The router dispatches through an ABCI query carrying an sdk.Context of its
// own, so a handler behind it never sees the client's context and could not
// observe a cancelled RPC. Registering the service directly keeps the real
// grpc-go lifecycle, which is what the release hook is being tested against.
type blockingQueryApp struct {
	startGRPCServerApp

	query *blockingQuery
}

func (a blockingQueryApp) RegisterGRPCServer(srv gogogrpc.Server) {
	testdata.RegisterQueryServer(srv, a.query)
}

// blockingServer is a live server whose Echo blocks, with the handles a test
// needs to drive it.
type blockingServer struct {
	srv      *grpc.Server
	addr     string
	registry *ratelimiter.Registry
	query    *blockingQuery
}

// startBlockingGRPCServer starts a real server through StartGRPCServer whose
// Echo blocks. It dials nothing, so a test counting connections counts its own.
func startBlockingGRPCServer(t *testing.T, cfg config.GRPCConfig) *blockingServer {
	t.Helper()

	app := baseapp.NewBaseApp(t.Name(), dbm.NewMemDB(), nil, nil, &testutil.TestAppOpts{})
	app.MountStores(sdk.NewKVStoreKey("test"))
	require.NoError(t, app.LoadLatestVersion())

	encCfg := moduletestutil.MakeTestEncodingConfig()
	testdata.RegisterInterfaces(encCfg.InterfaceRegistry)
	app.SetInterfaceRegistry(encCfg.InterfaceRegistry)

	clientCtx := client.Context{}.
		WithChainID("test-chain").
		WithTxConfig(encCfg.TxConfig).
		WithInterfaceRegistry(encCfg.InterfaceRegistry)

	query := newBlockingQuery()
	cfg.Address = freeTCPAddr(t)
	srv, registry, err := StartGRPCServer(clientCtx, blockingQueryApp{startGRPCServerApp{app}, query}, cfg)
	require.NoError(t, err)
	t.Cleanup(srv.Stop)
	t.Cleanup(query.releaseAll)

	return &blockingServer{srv: srv, addr: cfg.Address, registry: registry, query: query}
}

// dial returns a client on a new connection to the server.
func (s *blockingServer) dial(t *testing.T) testdata.QueryClient {
	t.Helper()
	conn, err := grpc.Dial(s.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return testdata.NewQueryClient(conn)
}

// echoAsync starts an Echo and reports its error on a channel.
func (s *blockingServer) echoAsync(ctx context.Context, client testdata.QueryClient) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		_, err := client.Echo(ctx, &testdata.EchoRequest{Message: "hello"})
		errCh <- err
	}()
	return errCh
}

// requireHeldSlots pins how many concurrency slots the loopback address holds.
func (s *blockingServer) requireHeldSlots(t *testing.T, want int) {
	t.Helper()
	require.Equal(t, want, s.registry.InFlightHeld("127.0.0.1"))
}

// requireEventuallyNoHeldSlots waits for a release, which happens on the
// server's own goroutine after the client has already seen the RPC end.
func (s *blockingServer) requireEventuallyNoHeldSlots(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		return s.registry.InFlightHeld("127.0.0.1") == 0
	}, 10*time.Second, 10*time.Millisecond, "a concurrency slot was never released")
}

// inFlightLimitedGRPCConfig leaves the token bucket wide enough that only the
// concurrency cap can reject.
func inFlightLimitedGRPCConfig(maxInFlight int) config.GRPCConfig {
	cfg := rateLimitedGRPCConfig(10_000, 10_000)
	cfg.MaxInFlightPerIP = maxInFlight
	return cfg
}

func requireEchoErr(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("an Echo never returned")
		return nil
	}
}

// TestStartGRPCServer_InFlightCapRejectsBeyondLimit is the concurrency bound the
// token bucket does not provide: with tokens to spare, a third overlapping RPC
// from one address is refused while two are still running.
func TestStartGRPCServer_InFlightCapRejectsBeyondLimit(t *testing.T) {
	reader := collectRejectionMetrics(t)
	const method = "testdata.Query/Echo"
	inFlightBefore := rejectionCounts(t, reader, inFlightRejectedMetric)[method]
	rateBefore := rejectionCounts(t, reader, rateLimitRejectedMetric)[method]

	server := startBlockingGRPCServer(t, inFlightLimitedGRPCConfig(2))
	client := server.dial(t)

	first := server.echoAsync(t.Context(), client)
	second := server.echoAsync(t.Context(), client)
	server.query.waitEntered(t, 2)

	_, err := client.Echo(t.Context(), &testdata.EchoRequest{Message: "hello"})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))

	// The rejection is attributed to the concurrency limit, not the bucket, so
	// an operator can tell the two controls apart.
	require.Equal(t, inFlightBefore+1, rejectionCounts(t, reader, inFlightRejectedMetric)[method])
	require.Equal(t, rateBefore, rejectionCounts(t, reader, rateLimitRejectedMetric)[method])

	server.query.releaseAll()
	require.NoError(t, requireEchoErr(t, first))
	require.NoError(t, requireEchoErr(t, second))
}

// TestStartGRPCServer_InFlightSlotReleasedWhenRPCEnds pins the release hook: a
// slot held by a finished RPC is available to the next one. A slot that leaked
// would lock the address out for as long as the process ran.
func TestStartGRPCServer_InFlightSlotReleasedWhenRPCEnds(t *testing.T) {
	server := startBlockingGRPCServer(t, inFlightLimitedGRPCConfig(1))
	client := server.dial(t)

	first := server.echoAsync(t.Context(), client)
	server.query.waitEntered(t, 1)
	server.requireHeldSlots(t, 1)

	_, err := client.Echo(t.Context(), &testdata.EchoRequest{Message: "hello"})
	require.Equal(t, codes.ResourceExhausted, status.Code(err))

	server.query.releaseAll()
	require.NoError(t, requireEchoErr(t, first))
	server.requireEventuallyNoHeldSlots(t)

	res, err := client.Echo(t.Context(), &testdata.EchoRequest{Message: "hello"})
	require.NoError(t, err)
	require.Equal(t, "hello", res.Message)
}

// TestStartGRPCServer_InFlightSlotReleasedOnClientCancel covers the termination
// path a served handler does not: the client abandons the RPC and the stream is
// reset. grpc-go emits the end-of-RPC event on that path too, which is why the
// release hangs off it rather than off the handler returning normally.
func TestStartGRPCServer_InFlightSlotReleasedOnClientCancel(t *testing.T) {
	server := startBlockingGRPCServer(t, inFlightLimitedGRPCConfig(1))
	client := server.dial(t)

	ctx, cancel := context.WithCancel(t.Context())
	first := server.echoAsync(ctx, client)
	server.query.waitEntered(t, 1)
	server.requireHeldSlots(t, 1)

	cancel()
	require.Error(t, requireEchoErr(t, first))
	server.requireEventuallyNoHeldSlots(t)
}

// TestStartGRPCServer_UnknownMethodCannotLeakInFlightSlots is the leak the cap
// would otherwise carry. grpc-go answers an unknown or malformed method name
// without emitting the end-of-RPC event the release hangs off, so a slot taken
// for one would never come back and the address would end up locked out
// entirely — a worse outcome than the burst the cap prevents.
func TestStartGRPCServer_UnknownMethodCannotLeakInFlightSlots(t *testing.T) {
	server := startBlockingGRPCServer(t, inFlightLimitedGRPCConfig(1))
	conn, err := grpc.Dial(server.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	// Both shapes grpc-go answers before it emits anything: a name with no
	// service segment, and a well-formed name for a service nobody registered.
	for _, method := range []string{"/nope", "/nope.Service/Nope"} {
		for range 10 {
			err := conn.Invoke(t.Context(), method, &testdata.EchoRequest{}, &testdata.EchoResponse{})
			require.Equal(t, codes.Unimplemented, status.Code(err), "method %q", method)
		}
	}

	server.requireHeldSlots(t, 0)

	// The address still has its whole allowance, which is what a leak would have
	// taken away.
	server.query.releaseAll()
	_, err = testdata.NewQueryClient(conn).Echo(t.Context(), &testdata.EchoRequest{Message: "hello"})
	require.NoError(t, err)
}

// TestStartGRPCWeb_SharesInFlightSlotsWithGRPC pins that gRPC-Web draws on the
// same per-IP pool as :9090 rather than a second one that would double an
// address's concurrency, and that it gives its slot back.
func TestStartGRPCWeb_SharesInFlightSlotsWithGRPC(t *testing.T) {
	server := startBlockingGRPCServer(t, inFlightLimitedGRPCConfig(1))
	require.NotNil(t, server.registry)
	client := server.dial(t)

	webAddr := freeTCPAddr(t)
	webSrv, err := StartGRPCWeb(server.srv, server.registry, config.Config{
		GRPCWeb: config.GRPCWebConfig{Enable: true, Address: webAddr},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = webSrv.Close() })

	// The native plane holds the address's only slot.
	first := server.echoAsync(t.Context(), client)
	server.query.waitEntered(t, 1)
	require.Equal(t, http.StatusTooManyRequests, postGRPCWeb(t, webAddr))

	server.query.releaseAll()
	require.NoError(t, requireEchoErr(t, first))
	server.requireEventuallyNoHeldSlots(t)

	// gRPC-Web hands its own slot back, so the pool is not drained by use. The
	// stats handler must not release it a second time, which would mint a slot.
	require.NotEqual(t, http.StatusTooManyRequests, postGRPCWeb(t, webAddr))
	server.requireEventuallyNoHeldSlots(t)
}

// TestStartGRPCServer_InFlightCapDisabledAdmitsOverlappingRPCs pins the off
// switch: a zero limit leaves overlapping RPCs to the token bucket alone.
func TestStartGRPCServer_InFlightCapDisabledAdmitsOverlappingRPCs(t *testing.T) {
	server := startBlockingGRPCServer(t, inFlightLimitedGRPCConfig(0))
	client := server.dial(t)

	running := make([]<-chan error, 0, 5)
	for range 5 {
		running = append(running, server.echoAsync(t.Context(), client))
	}
	server.query.waitEntered(t, 5)

	server.query.releaseAll()
	for _, errCh := range running {
		require.NoError(t, requireEchoErr(t, errCh))
	}
}

// TestStartGRPCServer_ConnectionsPerIPCapRefusesExcess bounds the layer below
// the RPC: the accepted sockets and HTTP/2 frame state one address can hold,
// which the per-RPC counter cannot see.
func TestStartGRPCServer_ConnectionsPerIPCapRefusesExcess(t *testing.T) {
	cfg := inFlightLimitedGRPCConfig(0)
	cfg.MaxConnectionsPerIP = 2
	server := startBlockingGRPCServer(t, cfg)

	held := dialRaw(t, server.addr)
	requireConnAlive(t, dialRaw(t, server.addr))
	requireServerHungUp(t, dialRaw(t, server.addr))

	// Closing a connection returns its slot.
	require.NoError(t, held.Close())
	requireEventuallyConnAlive(t, server.addr)
}

// TestStartGRPCServer_ConnectionsPerIPCapAppliesWithRateLimitingDisabled pins
// that the connection cap is independent of rate-limiting-enabled: it wraps the
// listener, which is below every control the rate-limit switch installs.
func TestStartGRPCServer_ConnectionsPerIPCapAppliesWithRateLimitingDisabled(t *testing.T) {
	cfg := inFlightLimitedGRPCConfig(0)
	cfg.RateLimitingEnabled = false
	cfg.MaxConnectionsPerIP = 1
	server := startBlockingGRPCServer(t, cfg)
	require.Nil(t, server.registry, "rate limiting is off, so there is no registry to admit against")

	requireConnAlive(t, dialRaw(t, server.addr))
	requireServerHungUp(t, dialRaw(t, server.addr))
}

// TestStartGRPCWeb_ConnectionsPerIPCapRefusesExcess covers the :9091 listener,
// which takes its cap from a config key of its own and enforces it with a
// counter of its own, so :9090 passing proves nothing about it.
func TestStartGRPCWeb_ConnectionsPerIPCapRefusesExcess(t *testing.T) {
	reader := collectRejectionMetrics(t)
	before := connRejectedCountsByPlane(t, reader)

	server := startBlockingGRPCServer(t, inFlightLimitedGRPCConfig(0))
	webAddr := freeTCPAddr(t)
	webSrv, err := StartGRPCWeb(server.srv, server.registry, config.Config{
		GRPCWeb: config.GRPCWebConfig{
			Enable:              true,
			Address:             webAddr,
			MaxConnectionsPerIP: 2,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = webSrv.Close() })

	held := dialRaw(t, webAddr)
	requireConnAlive(t, dialRaw(t, webAddr))
	requireServerHungUp(t, dialRaw(t, webAddr))

	// The refusal names the listener that made it, which is the only way an
	// operator can tell a :9091 cap from the :9090 one.
	after := connRejectedCountsByPlane(t, reader)
	require.Equal(t, before[ratelimiter.PlaneGRPCWeb]+1, after[ratelimiter.PlaneGRPCWeb])
	require.Equal(t, before[ratelimiter.PlaneGRPC], after[ratelimiter.PlaneGRPC])

	// Closing a connection returns its slot on this listener too.
	require.NoError(t, held.Close())
	requireEventuallyConnAlive(t, webAddr)
}

// connRejectedCountsByPlane returns the connection-rejection counter's value per
// plane label. The counter carries no method label, so rejectionCounts, which
// keys on method_namespace, cannot read it.
func connRejectedCountsByPlane(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	counts := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != connRejectedMetric {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				if v, ok := dp.Attributes.Value(attribute.Key("plane")); ok {
					counts[v.AsString()] += dp.Value
				}
			}
		}
	}
	return counts
}

func dialRaw(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// requireServerHungUp pins that the server closed conn rather than serving it,
// which is how an over-cap connection is refused.
func requireServerHungUp(t *testing.T, conn net.Conn) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
	_, err := conn.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
}

// requireConnAlive pins that the server kept conn: a connection under the cap is
// held open waiting for the client's HTTP/2 preface, so the read times out
// rather than reaching EOF.
func requireConnAlive(t *testing.T, conn net.Conn) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(500*time.Millisecond)))
	_, err := conn.Read(make([]byte, 1))
	require.NotErrorIs(t, err, io.EOF)
}

// requireEventuallyConnAlive requires addr to accept and retain a connection within five seconds.
func requireEventuallyConnAlive(t *testing.T, addr string) {
	t.Helper()
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		defer conn.Close()

		if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			return false
		}
		_, err = conn.Read(make([]byte, 1))
		if err == nil {
			return true
		}
		netErr, ok := err.(net.Error)
		return ok && netErr.Timeout()
	}, 5*time.Second, 10*time.Millisecond)
}
