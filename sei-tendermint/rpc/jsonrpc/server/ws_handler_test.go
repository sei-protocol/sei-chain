package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

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

	if got, want := dialResp.StatusCode, http.StatusSwitchingProtocols; got != want {
		t.Errorf("dialResp.StatusCode = %q, want %q", got, want)
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
	s := newEchoWSServer(t)
	defer s.Close()

	t.Cleanup(leaktest.Check(t))

	d := websocket.Dialer{}
	c, dialResp, err := d.Dial("ws://"+s.Listener.Addr().String()+"/websocket", nil)
	require.NoError(t, err)
	require.NoError(t, dialResp.Body.Close())

	// The first request asks for a response far larger than any socket buffer.
	// Wait until the response starts arriving, then stop reading from c; the
	// server's writeRoutine is now blocked on the socket write instead of still
	// preparing the large response.
	bigReq := rpctypes.NewRequest(1)
	require.NoError(t, bigReq.SetMethodAndParams("echo", map[string]any{"size": 32 * 1024 * 1024}))
	require.NoError(t, c.WriteJSON(bigReq))
	waitForResponseStart(t, c)

	// The follow-up tiny requests produce responses that fill writeChan to
	// capacity. readRoutine then blocks on the next send with no drainer.
	for i := range defaultWSWriteChanCapacity + 5 {
		req := rpctypes.NewRequest(i + 2)
		require.NoError(t, req.SetMethodAndParams("echo", map[string]any{"size": 0}))
		require.NoError(t, c.WriteJSON(req))
	}

	// Closing the client makes the writeRoutine's blocked socket write fail, so
	// it exits and the handler returns. A correct readRoutine must also unwind;
	// the buggy one stays blocked on the full writeChan -> leak.
	require.NoError(t, c.Close())
}

// TestWebsocketReadRoutineNoLeakOnPanicWithFullWriteChan exercises the panic
// recovery path in readRoutine. When a handler panics, recover() writes an error
// response into writeChan and then relaunches readRoutine. If writeChan is full
// and writeRoutine has stopped draining it, that recovery send must not block
// forever, and neither the recovering goroutine nor its relaunched successor may
// leak.
func TestWebsocketReadRoutineNoLeakOnPanicWithFullWriteChan(t *testing.T) {
	s := newEchoWSServer(t)
	defer s.Close()

	t.Cleanup(leaktest.Check(t))

	d := websocket.Dialer{}
	c, dialResp, err := d.Dial("ws://"+s.Listener.Addr().String()+"/websocket", nil)
	require.NoError(t, err)
	dialResp.Body.Close()

	// As in the full-writeChan test, read only enough of the huge first response
	// to know writeRoutine is blocked on the socket write and has stopped
	// draining writeChan.
	bigReq := rpctypes.NewRequest(1)
	require.NoError(t, bigReq.SetMethodAndParams("echo", map[string]any{"size": 32 * 1024 * 1024}))
	require.NoError(t, c.WriteJSON(bigReq))
	waitForResponseStart(t, c)

	// Exactly fill writeChan with tiny responses. writeRoutine pulls only the
	// big response (then blocks), so these defaultWSWriteChanCapacity sends all
	// succeed and leave writeChan full when readRoutine reads the next request.
	for i := range defaultWSWriteChanCapacity {
		req := rpctypes.NewRequest(i + 2)
		require.NoError(t, req.SetMethodAndParams("echo", map[string]any{"size": 0}))
		require.NoError(t, c.WriteJSON(req))
	}

	// The next request panics in the handler. readRoutine's recover() then tries
	// to push an error response into the already-full writeChan.
	panicReq := rpctypes.NewRequest(defaultWSWriteChanCapacity + 2)
	require.NoError(t, panicReq.SetMethodAndParams("panic", map[string]any{"size": 0}))
	require.NoError(t, c.WriteJSON(panicReq))

	// Closing the client makes writeRoutine's blocked write fail, cancelling the
	// connection context. That must release the recovery send and let any
	// relaunched readRoutine exit instead of leaking.
	require.NoError(t, c.Close())
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

func newEchoWSServer(t *testing.T) *httptest.Server {
	type sizeArgs struct {
		Size json.Number `json:"size"`
	}
	funcMap := map[string]*RPCFunc{
		"echo": NewWSRPCFunc(func(_ context.Context, a *sizeArgs) (string, error) {
			n, err := a.Size.Int64()
			if err != nil {
				return "", err
			}
			return strings.Repeat("x", int(n)), nil
		}),
		"panic": NewWSRPCFunc(func(_ context.Context, _ *sizeArgs) (string, error) {
			panic("boom in WSJSONRPC handler")
		}),
	}
	wm := NewWebsocketManager(funcMap)

	mux := http.NewServeMux()
	mux.HandleFunc("/websocket", wm.WebsocketHandler)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
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
