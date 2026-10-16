package p2p

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	dto "github.com/prometheus/client_model/go"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

const errMempoolFullMsg = "mempool is full: too many pending inserts"

// evmProxyTestService serves eth_sendRawTransaction for the proxy tests.
type evmProxyTestService struct {
	delay time.Duration
	err   error
	// block makes each call wait until its request is canceled.
	block bool
}

func (s *evmProxyTestService) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	if s.block {
		<-ctx.Done()
		return common.Hash{}, ctx.Err()
	}
	time.Sleep(s.delay)
	if s.err != nil {
		return common.Hash{}, s.err
	}
	return common.BytesToHash(input), nil
}

// startEvmProxyTestServer serves handler and counts the TCP connections it accepts.
func startEvmProxyTestServer(t *testing.T, handler http.Handler) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var conns atomic.Int64
	srv := httptest.NewUnstartedServer(handler)
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, &conns
}

// waitForCancel reads the request and blocks until the client goes away. The
// server notices a closed connection only after it has read the request body.
func waitForCancel(r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	<-r.Context().Done()
}

func startEvmProxyRPCServer(t *testing.T, svc *evmProxyTestService) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	server := ethrpc.NewServer()
	require.NoError(t, server.RegisterName("eth", svc))
	t.Cleanup(server.Stop)
	return startEvmProxyTestServer(t, server)
}

type evmProxyTestClient struct {
	client  *ethrpc.Client
	metrics *Metrics
	owner   string
}

func newEvmProxyTestClient(t *testing.T, url string, cfg evmProxyConfig) *evmProxyTestClient {
	t.Helper()
	owner := atypes.GenSecretKey(utils.TestRng()).Public()
	metrics := NewMetrics()
	client, transport, err := dialEvmProxy(t.Context(), owner, url, cfg, metrics)
	require.NoError(t, err)
	t.Cleanup(func() {
		client.Close()
		transport.close()
	})
	return &evmProxyTestClient{client: client, metrics: metrics, owner: owner.String()}
}

func (c *evmProxyTestClient) send(ctx context.Context, payload byte) (common.Hash, error) {
	var hash common.Hash
	err := c.client.CallContext(ctx, &hash, "eth_sendRawTransaction", hexutil.Bytes{payload})
	return hash, err
}

func (c *evmProxyTestClient) requests(t *testing.T, outcome string) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, c.metrics.evmProxyRequestsAt(c.owner, outcome).Write(&m))
	return m.GetCounter().GetValue()
}

func (c *evmProxyTestClient) newConns(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, c.metrics.evmProxyNewConnsAt(c.owner).Write(&m))
	return m.GetCounter().GetValue()
}

func (c *evmProxyTestClient) latencySamples(t *testing.T) uint64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, c.metrics.evmProxyRequestSecondsAt(c.owner).Write(&m))
	return m.GetHistogram().GetSampleCount()
}

func TestEvmProxyReusesConnections(t *testing.T) {
	const (
		maxConns   = 4
		sequential = 100
		workers    = 16
		perWorker  = 25
	)
	srv, conns := startEvmProxyRPCServer(t, &evmProxyTestService{delay: time.Millisecond})
	c := newEvmProxyTestClient(t, srv.URL, evmProxyConfig{maxConns: maxConns, timeout: 5 * time.Second})

	for i := range sequential {
		hash, err := c.send(t.Context(), byte(i))
		require.NoError(t, err)
		require.Equal(t, common.BytesToHash([]byte{byte(i)}), hash)
	}
	require.Equal(t, int64(1), conns.Load())

	var wg sync.WaitGroup
	errs := make(chan error, workers*perWorker)
	for range workers {
		wg.Go(func() {
			for i := range perWorker {
				_, err := c.send(t.Context(), byte(i))
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.LessOrEqual(t, conns.Load(), int64(maxConns))
	require.Equal(t, float64(conns.Load()), c.newConns(t))
	require.Equal(t, float64(sequential+workers*perWorker), c.requests(t, evmProxyOutcomeOK))
}

func TestEvmProxyTimesOutUnresponsiveOwner(t *testing.T) {
	const timeout = 200 * time.Millisecond
	srv, _ := startEvmProxyRPCServer(t, &evmProxyTestService{block: true})
	// One connection: the second request also times out while it waits for it.
	c := newEvmProxyTestClient(t, srv.URL, evmProxyConfig{maxConns: 1, timeout: timeout})

	start := time.Now()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := c.send(context.Background(), 1)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	require.Less(t, time.Since(start), 10*timeout)
	for err := range errs {
		require.ErrorIs(t, err, ErrEvmProxyTimeout)
	}
	require.Equal(t, 2.0, c.requests(t, evmProxyOutcomeTimeout))
}

func TestEvmProxyTimesOutStalledResponseBody(t *testing.T) {
	const timeout = 200 * time.Millisecond
	srv, _ := startEvmProxyTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	c := newEvmProxyTestClient(t, srv.URL, evmProxyConfig{maxConns: 1, timeout: timeout})

	start := time.Now()
	_, err := c.send(context.Background(), 1)
	require.ErrorIs(t, err, ErrEvmProxyTimeout)
	require.Less(t, time.Since(start), 10*timeout)
	require.Equal(t, 1.0, c.requests(t, evmProxyOutcomeTimeout))
}

func TestEvmProxyPassesOwnerErrorThrough(t *testing.T) {
	srv, _ := startEvmProxyRPCServer(t, &evmProxyTestService{err: errors.New(errMempoolFullMsg)})
	c := newEvmProxyTestClient(t, srv.URL, evmProxyConfig{maxConns: 1, timeout: 5 * time.Second})

	_, err := c.send(t.Context(), 1)
	require.Error(t, err)
	require.Equal(t, errMempoolFullMsg, err.Error())
	var rpcErr ethrpc.Error
	require.True(t, errors.As(err, &rpcErr))
	require.Equal(t, 1.0, c.requests(t, evmProxyOutcomeRPCError))
}

func TestEvmProxyMetricsByOutcome(t *testing.T) {
	var mode atomic.Value
	mode.Store(evmProxyOutcomeOK)
	rpcServer := ethrpc.NewServer()
	svc := &evmProxyTestService{}
	require.NoError(t, rpcServer.RegisterName("eth", svc))
	t.Cleanup(rpcServer.Stop)
	failing := &evmProxyTestService{err: errors.New(errMempoolFullMsg)}
	failingServer := ethrpc.NewServer()
	require.NoError(t, failingServer.RegisterName("eth", failing))
	t.Cleanup(failingServer.Stop)
	srv, _ := startEvmProxyTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load().(string) {
		case evmProxyOutcomeRPCError:
			failingServer.ServeHTTP(w, r)
		case evmProxyOutcomeTransportError:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case evmProxyOutcomeCanceled, evmProxyOutcomeTimeout:
			waitForCancel(r)
		default:
			rpcServer.ServeHTTP(w, r)
		}
	}))
	c := newEvmProxyTestClient(t, srv.URL, evmProxyConfig{maxConns: 2, timeout: 200 * time.Millisecond})

	_, err := c.send(t.Context(), 1)
	require.NoError(t, err)

	mode.Store(evmProxyOutcomeRPCError)
	_, err = c.send(t.Context(), 1)
	require.Error(t, err)

	mode.Store(evmProxyOutcomeTransportError)
	_, err = c.send(t.Context(), 1)
	var httpErr ethrpc.HTTPError
	require.True(t, errors.As(err, &httpErr))
	require.Equal(t, http.StatusServiceUnavailable, httpErr.StatusCode)

	mode.Store(evmProxyOutcomeTimeout)
	_, err = c.send(t.Context(), 1)
	require.ErrorIs(t, err, ErrEvmProxyTimeout)

	mode.Store(evmProxyOutcomeCanceled)
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(20*time.Millisecond, cancel)
	_, err = c.send(ctx, 1)
	require.ErrorIs(t, err, context.Canceled)

	for _, outcome := range []string{
		evmProxyOutcomeOK,
		evmProxyOutcomeRPCError,
		evmProxyOutcomeTransportError,
		evmProxyOutcomeTimeout,
	} {
		require.Equal(t, 1.0, c.requests(t, outcome), outcome)
	}
	// geth returns on caller cancellation before the transport records it.
	require.Eventually(t, func() bool { return c.requests(t, evmProxyOutcomeCanceled) == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, uint64(5), c.latencySamples(t))
}

func TestHasJSONRPCError(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{`{"jsonrpc":"2.0","id":1,"result":"0x01"}`, false},
		{`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"x"}}`, true},
		{`{"jsonrpc":"2.0","id":1,"error":null,"result":"0x01"}`, false},
		{`[{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"x"}}]`, false},
		{`{"jsonrpc":"2.0","id":1,"err`, false},
	} {
		require.Equal(t, tc.want, hasJSONRPCError([]byte(tc.body)), tc.body)
	}
}

func TestGigaRouterCommonConfigValidateEvmProxy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		maxConns utils.Option[uint64]
		timeout  utils.Option[time.Duration]
		ok       bool
	}{
		{"absent", utils.None[uint64](), utils.None[time.Duration](), true},
		{"set", utils.Some[uint64](8), utils.Some(time.Second), true},
		{"max", utils.Some[uint64](maxEvmProxyConnsPerOwner), utils.None[time.Duration](), true},
		{"zero_conns", utils.Some[uint64](0), utils.None[time.Duration](), false},
		{"too_many_conns", utils.Some[uint64](maxEvmProxyConnsPerOwner + 1), utils.None[time.Duration](), false},
		{"zero_timeout", utils.None[uint64](), utils.Some(time.Duration(0)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &GigaRouterCommonConfig{EvmProxyMaxConnsPerOwner: tc.maxConns, EvmProxyTimeout: tc.timeout}
			err := cfg.validateEvmProxy()
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	got := (&GigaRouterCommonConfig{}).evmProxyConfig()
	require.Equal(t, evmProxyConfig{maxConns: DefaultEvmProxyMaxConnsPerOwner, timeout: DefaultEvmProxyTimeout}, got)
}
