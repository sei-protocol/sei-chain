package evmrpc

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

type testRPCService struct{}

func (*testRPCService) Ping() string {
	return "pong"
}

func startTestRPCServer(t *testing.T) (string, *atomic.Int32, *atomic.Int32) {
	t.Helper()

	srv := rpc.NewServer()
	require.NoError(t, srv.RegisterName("test", &testRPCService{}))

	var newConns atomic.Int32
	var closedConns atomic.Int32
	ts := httptest.NewUnstartedServer(srv)
	ts.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateNew:
			newConns.Add(1)
		case http.StateClosed:
			closedConns.Add(1)
		}
	}
	ts.Start()
	t.Cleanup(ts.Close)

	return ts.URL, &newConns, &closedConns
}

func TestClientPool_Reuse(t *testing.T) {
	url, newConns, _ := startTestRPCServer(t)

	pool := newClientPool(time.Hour)
	t.Cleanup(pool.Stop)

	var result string
	require.NoError(t, pool.Call(t.Context(), url, &result, "test_ping"))
	require.Equal(t, "pong", result)
	require.NoError(t, pool.Call(t.Context(), url, &result, "test_ping"))
	require.Equal(t,newConns.Load(),1)
}

func TestClientPool_Expiration(t *testing.T) {
	url, opened, closed := startTestRPCServer(t)

	pool := newClientPool(time.Millisecond)
	t.Cleanup(pool.Stop)
	for range 3 {
		before := closed.Load()
		var result string
		require.NoError(t, pool.Call(t.Context(), url, &result, "test_ping"))
		require.Eventually(t, func() bool {
			return opened.Load()==closed.Load() && closed.Load()>before
		}, 10*time.Second, 25*time.Millisecond)
	}
}


func TestClientPool_Stop(t *testing.T) {
	url, opened, closed := startTestRPCServer(t)

	pool := newClientPool(time.Hour)

	var result string
	require.NoError(t, pool.Call(t.Context(), url, &result, "test_ping"))

	pool.Stop()
	require.Eventually(t, func() bool {
		return opened.Load() == closed.Load()	
	}, time.Second, 25*time.Millisecond)
}

func TestClientPool_StopIsIdempotent(t *testing.T) {
	pool := newClientPool(time.Minute)
	pool.Stop()
	pool.Stop()
}
