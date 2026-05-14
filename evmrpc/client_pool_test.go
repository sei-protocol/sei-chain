package evmrpc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	legacyabci "github.com/sei-protocol/sei-chain/app/legacyabci"
	"github.com/stretchr/testify/require"
)

type testRPCService struct{}

func (*testRPCService) Ping() string {
	return "pong"
}

func TestClientPoolReusesClients(t *testing.T) {
	server := rpc.NewServer()
	require.NoError(t, server.RegisterName("test", &testRPCService{}))

	dials := 0
	pool := newClientPool(time.Minute, func(context.Context, string) (*rpc.Client, error) {
		dials++
		return rpc.DialInProc(server), nil
	})
	t.Cleanup(pool.Stop)

	var result string
	require.NoError(t, pool.Call(t.Context(), "http://validator-1.example.com:8545", &result, "test_ping"))
	require.Equal(t, "pong", result)
	require.NoError(t, pool.Call(t.Context(), "http://validator-1.example.com:8545", &result, "test_ping"))
	require.Equal(t, 1, dials)
}

func TestClientPoolExtendsClientLifetimeOnReuse(t *testing.T) {
	server := rpc.NewServer()
	require.NoError(t, server.RegisterName("test", &testRPCService{}))

	dials := 0
	var closes atomic.Int32
	pool := newClientPoolWithHooks(
		20*time.Millisecond,
		func(context.Context, string) (*rpc.Client, error) {
			dials++
			return rpc.DialInProc(server), nil
		},
		func(client *rpc.Client) {
			closes.Add(1)
			client.Close()
		},
	)
	t.Cleanup(pool.Stop)

	client1, err := pool.getOrCreate(t.Context(), "http://validator-1.example.com:8545")
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	client2, err := pool.getOrCreate(t.Context(), "http://validator-1.example.com:8545")
	require.NoError(t, err)

	require.Same(t, client1, client2)
	require.Equal(t, 1, dials)

	time.Sleep(15 * time.Millisecond)
	require.Equal(t, int32(0), closes.Load())

	require.Eventually(t, func() bool {
		return closes.Load() == 1
	}, time.Second, 5*time.Millisecond)
}

func TestClientPoolClosesClientAfterLastLeaseExpires(t *testing.T) {
	server := rpc.NewServer()
	require.NoError(t, server.RegisterName("test", &testRPCService{}))

	dials := 0
	var closes atomic.Int32
	pool := newClientPoolWithHooks(
		10*time.Millisecond,
		func(context.Context, string) (*rpc.Client, error) {
			dials++
			return rpc.DialInProc(server), nil
		},
		func(client *rpc.Client) {
			closes.Add(1)
			client.Close()
		},
	)
	t.Cleanup(pool.Stop)

	client, err := pool.getOrCreate(t.Context(), "http://validator-1.example.com:8545")
	require.NoError(t, err)
	require.NotNil(t, client)
	require.Equal(t, 1, dials)

	require.Eventually(t, func() bool {
		pool.mu.Lock()
		_, ok := pool.clients["http://validator-1.example.com:8545"]
		pool.mu.Unlock()
		return !ok && closes.Load() == 1
	}, time.Second, 5*time.Millisecond)
}

func TestClientPoolStopExpiresLeasesImmediately(t *testing.T) {
	server := rpc.NewServer()
	require.NoError(t, server.RegisterName("test", &testRPCService{}))

	var closes atomic.Int32
	pool := newClientPoolWithHooks(
		time.Minute,
		func(context.Context, string) (*rpc.Client, error) {
			return rpc.DialInProc(server), nil
		},
		func(client *rpc.Client) {
			closes.Add(1)
			client.Close()
		},
	)

	_, err := pool.getOrCreate(t.Context(), "http://validator-1.example.com:8545")
	require.NoError(t, err)

	pool.Stop()
	require.Equal(t, int32(1), closes.Load())
}

func TestSendAPIAndTransactionAPIShareClientPool(t *testing.T) {
	pool := newClientPool(time.Minute, func(context.Context, string) (*rpc.Client, error) {
		return nil, nil
	})
	t.Cleanup(pool.Stop)
	sendAPI := NewSendAPI(nil, nil, &SendConfig{}, nil, legacyabci.BeginBlockKeepers{}, nil, "", nil, nil, nil, ConnectionTypeHTTP, pool, nil, nil, nil)
	txAPI := NewTransactionAPI(nil, nil, nil, nil, "", ConnectionTypeHTTP, pool, nil, nil, nil)

	require.Same(t, sendAPI.clientPool, txAPI.clientPool)
	require.Same(t, pool, sendAPI.clientPool)
}
