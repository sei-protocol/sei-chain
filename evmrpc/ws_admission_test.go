package evmrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

const errCodeBudgetWaitTimeout = -32005

type wsAdmissionTestService struct{}

func (wsAdmissionTestService) Sleep(_ context.Context, duration time.Duration) {
	time.Sleep(duration)
}

func TestEffectiveMaxRequestBodyBytes(t *testing.T) {
	require.Equal(t, defaultMaxRequestBodyBytes, effectiveMaxRequestBodyBytes(0))
	require.Equal(t, defaultMaxRequestBodyBytes, effectiveMaxRequestBodyBytes(-1))
	require.Equal(t, int64(1024), effectiveMaxRequestBodyBytes(1024))
}

func TestEnableWSConcurrentRequestBytes(t *testing.T) {
	const (
		sleepDuration = 200 * time.Millisecond
		pad           = 48
	)

	payload := makeSleepMsg(1, sleepDuration, pad)
	frameSize := int64(len(payload))

	srv := startWSTestServer(t, WsConfig{
		Origins: []string{"*"},
		RPCEndpointConfig: RPCEndpointConfig{
			readLimit:                 frameSize,
			maxConcurrentRequestBytes: frameSize,
		},
	})

	conn := dialWSTestServer(t, srv)
	defer conn.Close()

	writeWSJSON(t, conn, makeSleepMsg(1, sleepDuration, pad))
	writeWSJSON(t, conn, makeSleepMsg(2, sleepDuration, pad))

	var firstResp, secondResp rpcResponse
	start := time.Now()
	readJSON(t, conn, &firstResp)
	afterFirst := time.Since(start)
	readJSON(t, conn, &secondResp)
	total := time.Since(start)

	require.Nil(t, firstResp.Error)
	require.Nil(t, secondResp.Error)
	require.Equal(t, json.Number("1"), firstResp.ID)
	require.Equal(t, json.Number("2"), secondResp.ID)
	// With budget == frameSize only one handler runs at a time (~200ms each). Without
	// enforcement both finish concurrently (~200ms total, ~0ms between responses).
	require.GreaterOrEqual(t, afterFirst, sleepDuration/2)
	require.GreaterOrEqual(t, total-afterFirst, sleepDuration/2)
	require.GreaterOrEqual(t, total, 2*sleepDuration-sleepDuration/4)
}

func TestEnableWSConcurrentBudgetBelowReadLimitAdmitsMaxFrame(t *testing.T) {
	const pad = 48

	makeMsg := func(id int) string {
		return fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%d,"method":"test_sleep","params":[0],"_pad":"%s"}`,
			id, strings.Repeat("x", pad),
		)
	}
	payload := makeMsg(1)
	frameSize := int64(len(payload))

	srv := startWSTestServer(t, WsConfig{
		Origins: []string{"*"},
		RPCEndpointConfig: RPCEndpointConfig{
			readLimit:                 frameSize,
			maxConcurrentRequestBytes: frameSize / 2,
		},
	})

	conn := dialWSTestServer(t, srv)
	defer conn.Close()

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(payload)))

	var resp rpcResponse
	readJSON(t, conn, &resp)
	require.Equal(t, json.Number("1"), resp.ID)
	require.Nil(t, resp.Error)
}

func TestEnableWSRejectsOversizeFrame(t *testing.T) {
	const readLimit = 256

	srv := startWSTestServer(t, WsConfig{
		Origins: []string{"*"},
		RPCEndpointConfig: RPCEndpointConfig{
			readLimit:                 readLimit,
			maxConcurrentRequestBytes: 1024,
		},
	})

	conn := dialWSTestServer(t, srv)
	defer conn.Close()

	oversized := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"test_sleep","params":[0],"_pad":"%s"}`,
		strings.Repeat("x", readLimit),
	)
	writeWSJSON(t, conn, oversized)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err := conn.ReadMessage()
	require.Error(t, err, "expected connection to close after oversized frame")
}

func TestWSOversizeFrameFiresAdmissionHook(t *testing.T) {
	const (
		readLimit = 256
		budget    = 1024
	)

	srv := rpc.NewServer()
	require.NoError(t, srv.RegisterName("test", wsAdmissionTestService{}))

	var (
		mu      sync.Mutex
		reasons []string
	)
	srv.SetWSAdmissionEventHook(func(reason string) {
		recordWSAdmissionRejected(t.Context(), reason)
		mu.Lock()
		reasons = append(reasons, reason)
		mu.Unlock()
	})
	srv.SetReadLimits(readLimit)
	srv.SetWSConcurrentRequestBytes(budget)

	httpSrv := httptest.NewServer(srv.WebsocketHandler([]string{"*"}))
	t.Cleanup(func() {
		httpSrv.Close()
		srv.Stop()
	})

	conn, _, err := websocket.DefaultDialer.Dial(wsTestURL(httpSrv), nil)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	oversized := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"test_sleep","params":[0],"_pad":"%s"}`,
		strings.Repeat("x", readLimit),
	)
	writeWSJSON(t, conn, oversized)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err = conn.ReadMessage()
	require.Error(t, err, "expected connection to close after oversized frame")

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(reasons) == 1 && reasons[0] == rpc.WSAdmissionReasonOversizeFrame
	}, time.Second, 5*time.Millisecond)
}

func TestEnableWSAdmissionTimeout(t *testing.T) {
	const (
		sleepDuration = 200 * time.Millisecond
		pad           = 48
		waitTimeout   = 50 * time.Millisecond
	)

	payload := makeSleepMsg(1, sleepDuration, pad)
	frameSize := int64(len(payload))

	srv := startWSTestServer(t, WsConfig{
		Origins:            []string{"*"},
		wsAdmissionTimeout: waitTimeout,
		RPCEndpointConfig: RPCEndpointConfig{
			readLimit:                 frameSize,
			maxConcurrentRequestBytes: frameSize,
		},
	})

	conn := dialWSTestServer(t, srv)
	defer conn.Close()

	writeWSJSON(t, conn, makeSleepMsg(1, sleepDuration, pad))
	writeWSJSON(t, conn, makeSleepMsg(2, sleepDuration, pad))

	require.Eventually(t, func() bool {
		_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, data, err := conn.ReadMessage()
		_ = conn.SetReadDeadline(time.Time{})
		if err != nil {
			return false
		}
		var resp rpcResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return false
		}
		return resp.Error != nil && resp.Error.Code == errCodeBudgetWaitTimeout
	}, waitTimeout+300*time.Millisecond, 10*time.Millisecond)
}

func TestWSAdmissionHookBudgetWaitTimeout(t *testing.T) {
	const (
		sleepDuration = 200 * time.Millisecond
		pad           = 48
		waitTimeout   = 50 * time.Millisecond
	)

	srv := rpc.NewServer()
	require.NoError(t, srv.RegisterName("test", wsAdmissionTestService{}))

	var (
		mu      sync.Mutex
		reasons []string
	)
	srv.SetWSAdmissionEventHook(func(reason string) {
		recordWSAdmissionRejected(t.Context(), reason)
		mu.Lock()
		reasons = append(reasons, reason)
		mu.Unlock()
	})
	srv.SetWSAdmissionTimeout(waitTimeout)

	payload := makeSleepMsg(1, sleepDuration, pad)
	frameSize := int64(len(payload))
	srv.SetReadLimits(frameSize)
	srv.SetWSConcurrentRequestBytes(frameSize)

	httpsrv := httptest.NewServer(srv.WebsocketHandler([]string{"*"}))
	t.Cleanup(func() {
		httpsrv.Close()
		srv.Stop()
	})

	conn, _, err := websocket.DefaultDialer.Dial(wsTestURL(httpsrv), nil)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	writeWSJSON(t, conn, payload)

	var firstResp rpcResponse
	readJSON(t, conn, &firstResp)
	require.Nil(t, firstResp.Error)

	// After the first request completes the read loop blocks in NextReader without
	// holding budget. The hook must not fire while idle.
	idleDeadline := time.Now().Add(waitTimeout + 200*time.Millisecond)
	for time.Now().Before(idleDeadline) {
		mu.Lock()
		got := append([]string(nil), reasons...)
		mu.Unlock()
		require.Empty(t, got, "hook fired while idle in NextReader")
		time.Sleep(10 * time.Millisecond)
	}

	writeWSJSON(t, conn, makeSleepMsg(2, sleepDuration, pad))
	writeWSJSON(t, conn, makeSleepMsg(3, sleepDuration, pad))

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(reasons) > 0 && reasons[0] == rpc.WSAdmissionReasonBudgetWaitTimeout
	}, waitTimeout+300*time.Millisecond, 10*time.Millisecond)
}

type rpcResponse struct {
	ID      json.Number     `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
	JSONRPC string          `json:"jsonrpc"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func startWSTestServer(t *testing.T, wsConf WsConfig) *HTTPServer {
	t.Helper()

	srv := NewHTTPServer(rpc.DefaultHTTPTimeouts)
	require.NoError(t, srv.EnableWS([]rpc.API{
		{Namespace: "test", Service: wsAdmissionTestService{}},
	}, wsConf))
	require.NoError(t, srv.SetListenAddr("127.0.0.1", 0))
	require.NoError(t, srv.Start())
	t.Cleanup(func() { srv.Stop() })
	return srv
}

func dialWSTestServer(t *testing.T, srv *HTTPServer) *websocket.Conn {
	t.Helper()

	wsURL := "ws://" + srv.ListenAddr()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	return conn
}

func readJSON(t *testing.T, conn *websocket.Conn, dest *rpcResponse) {
	t.Helper()

	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	msgType, data, err := conn.ReadMessage()
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, dest))
	require.Equal(t, websocket.TextMessage, msgType)
	_ = conn.SetReadDeadline(time.Time{})
}

func wsTestURL(httpsrv *httptest.Server) string {
	return "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")
}

func writeWSJSON(t *testing.T, conn *websocket.Conn, payload string) {
	t.Helper()
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(payload)))
}

func makeSleepMsg(id int, sleep time.Duration, pad int) string {
	return fmt.Sprintf(
		`{"jsonrpc":"2.0","id":%d,"method":"test_sleep","params":[%d],"_pad":"%s"}`,
		id, sleep.Nanoseconds(), strings.Repeat("x", pad),
	)
}
