package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// serveWebsocket serves a handler enforcing limits over WebSocket, returning
// its ws:// URL.
func serveWebsocket(t *testing.T, backend Backend, limits gigaconfig.RPCConfig) string {
	t.Helper()
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), limits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(websocketHandler(handler))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

// rpcResponse is one JSON-RPC response as it crosses the wire.
type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func TestWebsocketSubscriptionLimits(t *testing.T) {
	executed := utils.NewAtomicSend(atypes.NewExecutedBlocks(atypes.ExecutedBlock{Number: 6}))
	backend := &testBackend{executedBlocks: func() (utils.AtomicRecv[atypes.ExecutedBlocks], error) {
		return executed.Subscribe(), nil
	}}
	url := serveWebsocket(t, backend, gigaconfig.RPCConfig{MaxSubscriptionsNewHead: 3, MaxSubscriptionsPerConnection: 2})
	dial := func() *ethrpc.Client {
		client, err := ethrpc.DialWebsocket(t.Context(), url, "")
		require.NoError(t, err)
		t.Cleanup(client.Close)
		return client
	}
	subscribe := func(client *ethrpc.Client) (*ethrpc.ClientSubscription, error) {
		return client.EthSubscribe(t.Context(), make(chan map[string]any), "newHeads")
	}
	// Slots free asynchronously once the server sees the unsubscribe or the
	// closed connection, so a subscription that should fit is retried.
	eventuallySubscribe := func(client *ethrpc.Client) *ethrpc.ClientSubscription {
		var sub *ethrpc.ClientSubscription
		require.Eventually(t, func() bool {
			var err error
			sub, err = subscribe(client)
			return err == nil
		}, 10*time.Second, 10*time.Millisecond)
		return sub
	}

	// A connection is refused past its own cap.
	a := dial()
	a1, err := subscribe(a)
	require.NoError(t, err)
	_, err = subscribe(a)
	require.NoError(t, err)
	_, err = subscribe(a)
	require.ErrorContains(t, err, errConnectionSubscriptionLimit.Error())

	// Unsubscribing frees a slot on the same connection.
	a1.Unsubscribe()
	eventuallySubscribe(a)

	// The node-wide cap counts every connection's subscriptions.
	b := dial()
	_, err = subscribe(b)
	require.NoError(t, err)
	c := dial()
	_, err = subscribe(c)
	require.ErrorContains(t, err, errNewHeadsSubscriptionLimit.Error())

	// Closing a connection frees all of its slots: c takes both of a's.
	a.Close()
	eventuallySubscribe(c)
	_, err = subscribe(c)
	require.NoError(t, err)
	_, err = subscribe(c)
	require.ErrorContains(t, err, errNewHeadsSubscriptionLimit.Error())
}

func TestWebsocketAdmissionTimeout(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limits gigaconfig.RPCConfig
		// wantTimeout is whether a request still being processed exhausts the
		// byte budget, so the connection is closed with -32005 instead of the
		// next request being served.
		wantTimeout bool
	}{
		{name: "disabled byte budget serves the next request", limits: gigaconfig.RPCConfig{}},
		{
			// The budget is raised to the 32 MiB WebSocket read limit, which a
			// connection reserves before reading each frame.
			name:        "exhausted byte budget times the connection out",
			limits:      gigaconfig.RPCConfig{MaxConcurrentRequestBytes: 1, WSAdmissionTimeout: 50 * time.Millisecond},
			wantTimeout: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entered := make(chan struct{})
			unblock := make(chan struct{})
			release := sync.OnceFunc(func() { close(unblock) })
			t.Cleanup(release)
			backend := &testBackend{
				blockNumber: func() uint64 { return 42 },
				// eth_chainId stays in flight until the test ends.
				chainID: func() uint64 {
					close(entered)
					<-unblock
					return 713715
				},
			}
			conn, resp, err := websocket.DefaultDialer.DialContext(t.Context(), serveWebsocket(t, backend, tc.limits), nil)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			t.Cleanup(func() { _ = conn.Close() })

			require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","id":1,"method":"eth_chainId"}`)))
			<-entered
			// With the budget exhausted the server may already have closed the
			// connection, so only the served case requires the write to land.
			writeErr := conn.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","id":2,"method":"eth_blockNumber"}`))
			if !tc.wantTimeout {
				require.NoError(t, writeErr)
			}

			// Far below go-ethereum's 30s default, so only the configured
			// admission timeout can answer in time.
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
			_, raw, err := conn.ReadMessage()
			require.NoError(t, err)
			var got rpcResponse
			require.NoError(t, json.Unmarshal(raw, &got))
			if tc.wantTimeout {
				require.NotNil(t, got.Error, "response %s", raw)
				require.Equal(t, -32005, got.Error.Code)
				// The connection closes once its in-flight request returns, after
				// delivering that request's answer.
				release()
				_, raw, err = conn.ReadMessage()
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(raw, &got))
				require.JSONEq(t, `1`, string(got.ID))
				_, _, err = conn.ReadMessage()
				require.Error(t, err, "the connection must close after the timeout")
				return
			}
			require.Nil(t, got.Error, "response %s", raw)
			require.JSONEq(t, `2`, string(got.ID))
			require.JSONEq(t, `"0x2a"`, string(got.Result))
		})
	}
}

func TestBatchLimits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		limits   gigaconfig.RPCConfig
		requests int
		// wantCodes is each response's error code, 0 for a result.
		wantCodes []int
	}{
		{name: "disabled limits serve the whole batch", requests: 3, wantCodes: []int{0, 0, 0}},
		{name: "batch at the request limit", limits: gigaconfig.RPCConfig{BatchRequestLimit: 2}, requests: 2, wantCodes: []int{0, 0}},
		// One error answers the whole batch.
		{name: "batch over the request limit", limits: gigaconfig.RPCConfig{BatchRequestLimit: 2}, requests: 3, wantCodes: []int{-32600}},
		// The response that crosses the size limit is kept; the rest are refused.
		{name: "batch over the response size limit", limits: gigaconfig.RPCConfig{BatchResponseMaxSize: 1}, requests: 3, wantCodes: []int{0, -32003, -32003}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &testBackend{blockNumber: func() uint64 { return 42 }}
			handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), tc.limits)
			require.NoError(t, err)
			t.Cleanup(handler.Stop)
			server := httptest.NewServer(handler)
			t.Cleanup(server.Close)

			batch := make([]map[string]any, tc.requests)
			for i := range batch {
				batch[i] = map[string]any{"jsonrpc": "2.0", "id": i + 1, "method": "eth_blockNumber"}
			}
			body, err := json.Marshal(batch)
			require.NoError(t, err)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, bytes.NewReader(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			t.Cleanup(func() { _ = resp.Body.Close() })

			var got []rpcResponse
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
			codes := make([]int, len(got))
			for i, r := range got {
				if r.Error != nil {
					codes[i] = r.Error.Code
				}
			}
			require.Equal(t, tc.wantCodes, codes)
		})
	}
}

func TestStartLimitsWebsocketConnections(t *testing.T) {
	for _, tc := range []struct {
		name           string
		maxConnections int
		wantSecondWait bool
	}{
		{name: "a second connection waits for the first to close", maxConnections: 1, wantSecondWait: true},
		{name: "zero disables the limit", maxConnections: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &testBackend{blockNumber: func() uint64 { return 42 }}
			server, err := Start(backend, evmonly.NewMemoryReceiptStore(), 0, 0, gigaconfig.RPCConfig{MaxWSConnections: tc.maxConnections})
			require.NoError(t, err)
			t.Cleanup(server.Stop)
			go func() { _ = server.Serve(t.Context()) }()
			url := "ws://" + server.wsListener.Addr().String()
			blockNumber := func(client *ethrpc.Client) {
				var got hexutil.Uint64
				require.NoError(t, client.CallContext(t.Context(), &got, "eth_blockNumber"))
				require.Equal(t, hexutil.Uint64(42), got)
			}

			first, err := ethrpc.DialWebsocket(t.Context(), url, "")
			require.NoError(t, err)
			blockNumber(first)

			// The capped listener never accepts the second connection while the
			// first is open, so its handshake runs out the dial deadline.
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			second, err := ethrpc.DialWebsocket(ctx, url, "")
			if !tc.wantSecondWait {
				require.NoError(t, err)
				blockNumber(second)
				second.Close()
				first.Close()
				return
			}
			require.Error(t, err)

			first.Close()
			third, err := ethrpc.DialWebsocket(t.Context(), url, "")
			require.NoError(t, err)
			t.Cleanup(third.Close)
			blockNumber(third)
		})
	}
}
