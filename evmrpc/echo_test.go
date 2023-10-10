package evmrpc

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
)

const TestAddr = "127.0.0.1"
const TestPort = 7777
const TestWSPort = 7778

func TestEcho(t *testing.T) {
	// Test HTTP server
	httpServer, err := NewEVMHTTPServer(log.NewNopLogger(), TestAddr, TestPort, rpc.DefaultHTTPTimeouts)
	require.Nil(t, err)
	require.Nil(t, httpServer.Start())
	// wait for a second in case the actual server goroutine isn't ready yet
	time.Sleep(1 * time.Second)
	body := "{\"jsonrpc\": \"2.0\",\"method\": \"echo_echo\",\"params\":[\"something\"],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":\"something\"}\n", string(resBody))

	// Test WS server
	wsServer, err := NewEVMWebSocketServer(log.NewNopLogger(), TestAddr, TestWSPort, []string{"localhost"}, rpc.DefaultHTTPTimeouts)
	require.Nil(t, err)
	require.Nil(t, wsServer.Start())
	// wait for a second in case the actual server goroutine isn't ready yet
	time.Sleep(1 * time.Second)
	headers := make(http.Header)
	headers.Set("Origin", "localhost")
	headers.Set("Content-Type", "application/json")
	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://%s:%d", TestAddr, TestWSPort), headers)
	require.Nil(t, err)
	require.Nil(t, conn.WriteMessage(websocket.TextMessage, []byte(body)))
	_, buf, err := conn.ReadMessage()
	require.Nil(t, err)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"id\":\"test\",\"result\":\"something\"}\n", string(buf))
}
