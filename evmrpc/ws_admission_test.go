package evmrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

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

	makeMsg := func(id int, method string) string {
		return fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%d,"method":"%s","params":[%d],"_pad":"%s"}`,
			id, method, sleepDuration.Nanoseconds(), strings.Repeat("x", pad),
		)
	}
	payload := makeMsg(1, "test_sleep")
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

	writeReq := func(id int) {
		msg := makeMsg(id, "test_sleep")
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(msg)))
	}

	writeReq(1)
	writeReq(2)

	var firstResp, secondResp rpcResponse
	readJSON(t, conn, &firstResp)
	readJSON(t, conn, &secondResp)

	require.Equal(t, json.Number("1"), firstResp.ID)
	require.Equal(t, json.Number("2"), secondResp.ID)
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

func TestEnableWSReadLimitDefault(t *testing.T) {
	srv := NewHTTPServer(rpc.DefaultHTTPTimeouts)
	wsConf := WsConfig{
		Origins: []string{"*"},
		RPCEndpointConfig: RPCEndpointConfig{
			readLimit:                 0,
			maxConcurrentRequestBytes: 0,
		},
	}
	require.NoError(t, srv.EnableWS([]rpc.API{
		{Namespace: "test", Service: wsAdmissionTestService{}},
	}, wsConf))
	require.Equal(t, defaultMaxRequestBodyBytes, effectiveMaxRequestBodyBytes(wsConf.readLimit))
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

	makeMsg := func(id int) string {
		return fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%d,"method":"test_sleep","params":[%d],"_pad":"%s"}`,
			id, sleepDuration.Nanoseconds(), strings.Repeat("x", pad),
		)
	}
	payload := makeMsg(1)
	frameSize := int64(len(payload))
	srv.SetReadLimits(frameSize)
	srv.SetWSConcurrentRequestBytes(frameSize)

	p1, p2 := net.Pipe()
	serveDone := make(chan struct{})
	go func() {
		srv.ServeCodec(rpc.NewCodec(p1), 0)
		close(serveDone)
	}()
	t.Cleanup(func() {
		p2.Close()
		p1.Close()
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Error("ServeCodec did not exit within 2s")
		}
		srv.Stop()
	})

	_, err := io.WriteString(p2, payload)
	require.NoError(t, err)

	deadline := time.Now().Add(waitTimeout + 300*time.Millisecond)
	for {
		mu.Lock()
		got := append([]string(nil), reasons...)
		mu.Unlock()
		if len(got) > 0 {
			require.Equal(t, rpc.WSAdmissionReasonBudgetWaitTimeout, got[0])
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("expected admission hook to fire on budget wait timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
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
	require.IsType(t, websocket.TextMessage, msgType)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, dest))
	_ = conn.SetReadDeadline(time.Time{})
}
