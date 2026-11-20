package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gorilla/websocket"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	rpctypes "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/jsonrpc/types"
)

func TestWebsocketManagerHandler(t *testing.T) {

	s := newWSServer(t)
	defer s.Close()

	t.Cleanup(leaktest.Check(t))

	// check upgrader works
	d := websocket.Dialer{}
	c, dialResp, err := d.Dial("ws://"+s.Listener.Addr().String()+"/websocket", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	if got, want := dialResp.StatusCode, http.StatusSwitchingProtocols; got != want {
		t.Errorf("dialResp.StatusCode = %d, want %d", got, want)
	}

	// check basic functionality works
	req := rpctypes.NewRequest(1001)
	require.NoError(t, req.SetMethodAndParams("c", map[string]interface{}{"s": "a", "i": 10}))
	require.NoError(t, c.WriteJSON(req))

	var resp rpctypes.RPCResponse
	err = c.ReadJSON(&resp)
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	dialResp.Body.Close()
}

// TestWebsocketReadRoutineNoLeakOnFullWriteChan reproduces a goroutine leak in
// readRoutine: when writeChan is full and the writeRoutine has stopped draining
// it, readRoutine blocks forever in WriteRPCResponse because it pushes responses
// using a non-cancelable context. The connection then tears down (writeRoutine
// exits, the handler returns) but readRoutine is stranded on the channel send.
func TestWebsocketReadRoutineNoLeakOnFullWriteChan(t *testing.T) {
	checkReadRoutineStopsWithFullWriteChan(t, "mark")
}

// TestWebsocketReadRoutineNoLeakOnPanicWithFullWriteChan exercises the panic
// recovery path in readRoutine. When a handler panics, recover() writes an error
// response into writeChan and then relaunches readRoutine. If writeChan is full
// and writeRoutine has stopped draining it, that recovery send must not block
// forever, and neither the recovering goroutine nor its relaunched successor may
// leak.
func TestWebsocketReadRoutineNoLeakOnPanicWithFullWriteChan(t *testing.T) {
	checkReadRoutineStopsWithFullWriteChan(t, "panic")
}

func checkReadRoutineStopsWithFullWriteChan(t *testing.T, finalMethod string) {
	t.Helper()

	s := newEchoWSServer(t)

	t.Cleanup(leaktest.Check(t))

	d := websocket.Dialer{}
	c, dialResp, err := d.Dial("ws://"+s.Listener.Addr().String()+"/websocket", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	serverConn, err := utils.Recv(s.handlerCtx, s.serverConns)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverConn.Close() })
	require.NoError(t, dialResp.Body.Close())

	// Wait for the oversized response to start, then stop reading so writeRoutine
	// stays in the socket write and cannot drain writeChan.
	bigReq := rpctypes.NewRequest(1)
	require.NoError(t, bigReq.SetMethodAndParams("echo", map[string]any{"size": 32 * 1024 * 1024}))
	require.NoError(t, c.WriteJSON(bigReq))
	waitForResponseStart(t, c)

	// readRoutine enqueues these responses before handling the final request.
	for i := range defaultWSWriteChanCapacity {
		req := rpctypes.NewRequest(i + 2)
		require.NoError(t, req.SetMethodAndParams("echo", map[string]any{"size": 0}))
		require.NoError(t, c.WriteJSON(req))
	}

	// Both handlers signal before readRoutine attempts its final send, whether
	// that send is the normal response or the panic recovery response.
	finalReq := rpctypes.NewRequest(defaultWSWriteChanCapacity + 2)
	require.NoError(t, finalReq.SetMethodAndParams(finalMethod, map[string]any{"size": 0}))
	require.NoError(t, c.WriteJSON(finalReq))
	_, err = utils.Recv(s.handlerCtx, s.marks)
	require.NoError(t, err, "websocket handler exited before the %q RPC marker", finalMethod)

	// Closing the server's socket interrupts the pending write directly.
	// When writeRoutine exits, readRoutine must release its send to the full
	// writeChan and unwind.
	require.NoError(t, serverConn.Close())
}

func waitForResponseStart(t *testing.T, c *websocket.Conn) {
	t.Helper()

	require.NoError(t, c.SetReadDeadline(time.Now().Add(defaultWSWriteWait)))
	_, r, err := c.NextReader()
	require.NoError(t, err)

	var b [1]byte
	n, err := r.Read(b[:])
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.NoError(t, c.SetReadDeadline(time.Time{}))
}

// echoWSServer is a websocket test server exposing "echo", "mark" and "panic" methods.
type echoWSServer struct {
	*httptest.Server
	// handlerCtx is canceled when the WebSocket handler returns.
	handlerCtx context.Context
	// serverConns yields the server's end of the upgraded connection.
	serverConns <-chan net.Conn
	// marks receives a value when the "mark" or "panic" method is called.
	marks <-chan struct{}
}

func newEchoWSServer(t *testing.T) *echoWSServer {
	type sizeArgs struct {
		Size json.Number `json:"size"`
	}
	marks := make(chan struct{}, 1)
	funcMap := map[string]*RPCFunc{
		"echo": NewWSRPCFunc(func(_ context.Context, a *sizeArgs) (string, error) {
			n, err := a.Size.Int64()
			if err != nil {
				return "", err
			}
			return strings.Repeat("x", int(n)), nil
		}),
		"mark": NewWSRPCFunc(func(_ context.Context, _ *sizeArgs) (string, error) {
			_ = utils.SendOrDrop(marks, struct{}{})
			return "", nil
		}),
		"panic": NewWSRPCFunc(func(_ context.Context, _ *sizeArgs) (string, error) {
			_ = utils.SendOrDrop(marks, struct{}{})
			panic("boom in WSJSONRPC handler")
		}),
	}
	wm := NewWebsocketManager(funcMap)

	handlerCtx, handlerExited := context.WithCancel(t.Context())
	t.Cleanup(handlerExited)
	mux := http.NewServeMux()
	mux.HandleFunc("/websocket", func(w http.ResponseWriter, r *http.Request) {
		defer handlerExited()
		wm.WebsocketHandler(w, r)
	})

	serverConns := make(chan net.Conn, 1)
	srv := httptest.NewUnstartedServer(mux)
	srv.Config.ConnState = func(c net.Conn, state http.ConnState) {
		if state == http.StateHijacked {
			_ = utils.SendOrDrop(serverConns, c)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	return &echoWSServer{Server: srv, handlerCtx: handlerCtx, serverConns: serverConns, marks: marks}
}

func newWSServer(t *testing.T) *httptest.Server {
	type args struct {
		S string      `json:"s"`
		I json.Number `json:"i"`
	}
	funcMap := map[string]*RPCFunc{
		"c": NewWSRPCFunc(func(context.Context, *args) (string, error) { return "foo", nil }),
	}
	wm := NewWebsocketManager(funcMap)

	mux := http.NewServeMux()
	mux.HandleFunc("/websocket", wm.WebsocketHandler)

	srv := httptest.NewServer(mux)

	t.Cleanup(srv.Close)

	return srv
}
