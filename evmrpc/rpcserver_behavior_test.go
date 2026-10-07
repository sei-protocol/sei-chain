package evmrpc

// Behavior tests pinning go-ethereum rpc.Server deadline hook and batch limit as wired by HTTPServer.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	"github.com/sei-protocol/sei-chain/ratelimiter"
	"github.com/stretchr/testify/require"
)

// behaviorRPCService is registered under both the "test" and "eth" namespaces.
type behaviorRPCService struct {
	subscribeHadDeadline chan bool
}

func (behaviorRPCService) Echo(s string) string { return s }

// Wait blocks until ctx is done (returning its error) or 2s pass.
func (behaviorRPCService) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
		return nil
	}
}

// HasDeadline reports whether the dispatch context carries a deadline.
func (behaviorRPCService) HasDeadline(ctx context.Context) bool {
	_, ok := ctx.Deadline()
	return ok
}

// Fast records whether the subscribe context has a deadline.
func (s behaviorRPCService) Fast(ctx context.Context) (*rpc.Subscription, error) {
	_, ok := ctx.Deadline()
	s.subscribeHadDeadline <- ok
	n, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return nil, rpc.ErrNotificationsUnsupported
	}
	return n.CreateSubscription(), nil
}

// Slow blocks subscription setup past the subscribe deadline.
func (behaviorRPCService) Slow(ctx context.Context) (*rpc.Subscription, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
	}
	n, _ := rpc.NotifierFromContext(ctx)
	return n.CreateSubscription(), nil
}

func behaviorAPIs(svc behaviorRPCService) []rpc.API {
	return []rpc.API{
		{Namespace: "test", Service: svc},
		{Namespace: "eth", Service: svc},
	}
}

// installBehaviorDeadlines installs a global deadline enforcer for the test.
func installBehaviorDeadlines(t *testing.T, cfg ratelimiter.DeadlineConfig) {
	t.Helper()
	prev := globalDeadlineEnforcer.Load()
	InitGlobalDeadlineEnforcer(ratelimiter.NewDeadlineEnforcer(cfg))
	t.Cleanup(func() { globalDeadlineEnforcer.Store(prev) })
}

func startBehaviorHTTPServer(t *testing.T, apis []rpc.API, cfg HTTPConfig) string {
	t.Helper()
	srv := NewHTTPServer(rpc.DefaultHTTPTimeouts)
	require.NoError(t, srv.EnableRPC(apis, cfg))
	require.NoError(t, srv.SetListenAddr("127.0.0.1", 0))
	require.NoError(t, srv.Start())
	t.Cleanup(srv.Stop)
	return "http://" + srv.ListenAddr()
}

func startBehaviorWSServer(t *testing.T, apis []rpc.API, cfg WsConfig) *websocket.Conn {
	t.Helper()
	srv := NewHTTPServer(rpc.DefaultHTTPTimeouts)
	require.NoError(t, srv.EnableWS(apis, cfg))
	require.NoError(t, srv.SetListenAddr("127.0.0.1", 0))
	require.NoError(t, srv.Start())
	t.Cleanup(srv.Stop)
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+srv.ListenAddr(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func postBehaviorJSON(t *testing.T, url, body string) string {
	t.Helper()
	res, err := http.Post(url, "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	bz, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return string(bytes.TrimSpace(bz))
}

func wsBehaviorRoundTrip(t *testing.T, conn *websocket.Conn, body string) string {
	t.Helper()
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(body)))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)
	return string(bytes.TrimSpace(data))
}

func behaviorBatch(n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"test_echo","params":["x%d"]}`, i+1, i+1)
	}
	return "[" + strings.Join(items, ",") + "]"
}

// Per-method deadlines apply to HTTP calls and batch elements.
func TestBehaviorDeadlineHookHTTP(t *testing.T) {
	installBehaviorDeadlines(t, ratelimiter.DeadlineConfig{
		Overrides: map[string]time.Duration{"test_wait": 50 * time.Millisecond},
	})
	url := startBehaviorHTTPServer(t, behaviorAPIs(behaviorRPCService{}), HTTPConfig{Vhosts: []string{"*"}, CorsAllowedOrigins: []string{"*"}})

	// per-method deadline yields -32002
	start := time.Now()
	require.Equal(t, `{"jsonrpc":"2.0","id":1,"error":{"code":-32002,"message":"request timed out"}}`,
		postBehaviorJSON(t, url, `{"jsonrpc":"2.0","id":1,"method":"test_wait","params":[]}`))
	require.Less(t, time.Since(start), time.Second)

	// no override and no default: no deadline
	require.Equal(t, `{"jsonrpc":"2.0","id":1,"result":false}`,
		postBehaviorJSON(t, url, `{"jsonrpc":"2.0","id":1,"method":"test_hasDeadline","params":[]}`))

	// per batch element
	require.Equal(t, `[{"jsonrpc":"2.0","id":1,"error":{"code":-32002,"message":"request timed out"}},{"jsonrpc":"2.0","id":2,"result":"ok"}]`,
		postBehaviorJSON(t, url, `[{"jsonrpc":"2.0","id":1,"method":"test_wait","params":[]},{"jsonrpc":"2.0","id":2,"method":"test_echo","params":["ok"]}]`))
}

// Deadlines apply to WS eth_subscribe, eth_unsubscribe and plain calls.
func TestBehaviorDeadlineHookWSSubscribeUnsubscribe(t *testing.T) {
	installBehaviorDeadlines(t, ratelimiter.DeadlineConfig{
		Overrides: map[string]time.Duration{
			"eth_subscribe":   200 * time.Millisecond,
			"eth_unsubscribe": time.Nanosecond,
		},
	})
	svc := behaviorRPCService{subscribeHadDeadline: make(chan bool, 4)}
	conn := startBehaviorWSServer(t, behaviorAPIs(svc), WsConfig{Origins: []string{"*"}})

	// subscribe setup carries a deadline
	var sub struct {
		Result string `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(wsBehaviorRoundTrip(t, conn, `{"jsonrpc":"2.0","id":1,"method":"eth_subscribe","params":["fast"]}`)), &sub))
	require.NotEmpty(t, sub.Result)
	select {
	case had := <-svc.subscribeHadDeadline:
		require.True(t, had, "eth_subscribe setup context should carry the configured deadline")
	case <-time.After(time.Second):
		t.Fatal("subscription callback not invoked")
	}

	// slow subscribe setup yields -32002
	require.Equal(t, `{"jsonrpc":"2.0","id":2,"error":{"code":-32002,"message":"request timed out"}}`,
		wsBehaviorRoundTrip(t, conn, `{"jsonrpc":"2.0","id":2,"method":"eth_subscribe","params":["slow"]}`))

	// unsubscribe succeeds despite an expired deadline
	require.Equal(t, `{"jsonrpc":"2.0","id":3,"result":true}`,
		wsBehaviorRoundTrip(t, conn, fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"eth_unsubscribe","params":["%s"]}`, sub.Result)))
	// unknown id: timeout error replaces "subscription not found"
	require.Equal(t, `{"jsonrpc":"2.0","id":4,"error":{"code":-32002,"message":"request timed out"}}`,
		wsBehaviorRoundTrip(t, conn, fmt.Sprintf(`{"jsonrpc":"2.0","id":4,"method":"eth_unsubscribe","params":["%s"]}`, sub.Result)))

	// plain WS call
	require.Equal(t, `{"jsonrpc":"2.0","id":5,"result":false}`,
		wsBehaviorRoundTrip(t, conn, `{"jsonrpc":"2.0","id":5,"method":"test_hasDeadline","params":[]}`))
}

// Default deadline config per method.
func TestBehaviorDefaultDeadlineConfig(t *testing.T) {
	cfg, err := evmrpcconfig.DefaultConfig.DeadlineEnforcerConfig()
	require.NoError(t, err)
	e := ratelimiter.NewDeadlineEnforcer(cfg)
	sim := evmrpcconfig.DefaultConfig.SimulationEVMTimeout
	require.Equal(t, sim, e.Deadline("eth_call"))
	require.Equal(t, sim, e.Deadline("eth_estimateGas"))
	require.Equal(t, sim, e.Deadline("eth_createAccessList"))
	require.Equal(t, time.Duration(0), e.Deadline("eth_sendRawTransaction"))
	require.Equal(t, time.Duration(0), e.Deadline("debug_traceBlockByNumber"))
	require.Equal(t, evmrpcconfig.DefaultConfig.RPCDefaultTimeout, e.Deadline("eth_subscribe"))
	require.Equal(t, evmrpcconfig.DefaultConfig.RPCDefaultTimeout, e.Deadline("eth_unsubscribe"))
}

const batchTooLargeResp = `[{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"batch too large"}}]`

// HTTP batches over the item limit are rejected.
func TestBehaviorBatchItemLimitHTTP(t *testing.T) {
	require.Equal(t, 100, evmrpcconfig.DefaultConfig.BatchRequestLimit)
	installBehaviorDeadlines(t, ratelimiter.DeadlineConfig{})

	cfg := HTTPConfig{Vhosts: []string{"*"}, CorsAllowedOrigins: []string{"*"}}
	cfg.batchItemLimit = 3
	url := startBehaviorHTTPServer(t, behaviorAPIs(behaviorRPCService{}), cfg)

	require.Equal(t, `[{"jsonrpc":"2.0","id":1,"result":"x1"},{"jsonrpc":"2.0","id":2,"result":"x2"},{"jsonrpc":"2.0","id":3,"result":"x3"}]`,
		postBehaviorJSON(t, url, behaviorBatch(3)))
	require.Equal(t, batchTooLargeResp, postBehaviorJSON(t, url, behaviorBatch(4)))
}

// WS batches over the item limit are rejected.
func TestBehaviorBatchItemLimitWS(t *testing.T) {
	installBehaviorDeadlines(t, ratelimiter.DeadlineConfig{})
	cfg := WsConfig{Origins: []string{"*"}}
	cfg.batchItemLimit = 3
	conn := startBehaviorWSServer(t, behaviorAPIs(behaviorRPCService{}), cfg)

	require.Equal(t, `[{"jsonrpc":"2.0","id":1,"result":"x1"},{"jsonrpc":"2.0","id":2,"result":"x2"},{"jsonrpc":"2.0","id":3,"result":"x3"}]`,
		wsBehaviorRoundTrip(t, conn, behaviorBatch(3)))
	require.Equal(t, batchTooLargeResp, wsBehaviorRoundTrip(t, conn, behaviorBatch(4)))
}
