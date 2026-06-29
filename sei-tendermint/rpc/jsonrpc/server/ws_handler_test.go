package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	// We deliberately never read any response from c. The first request asks for
	// a response far larger than any socket buffer; the server's writeRoutine
	// pulls that response first and blocks on the socket write, so it stops
	// draining writeChan.
	bigReq := rpctypes.NewRequest(1)
	require.NoError(t, bigReq.SetMethodAndParams("echo", map[string]any{"size": 32 * 1024 * 1024}))
	require.NoError(t, c.WriteJSON(bigReq))

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
