package rpc

import (
	"context"
	"errors"
	"math/big"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

func TestSendRawTransaction(t *testing.T) {
	tx, raw := testSignedTransaction(t)
	var broadcastRaw []byte
	backend := &testBackend{
		broadcast: func(_ context.Context, req *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			broadcastRaw = append([]byte(nil), req.Tx...)
			return &coretypes.ResultBroadcastTx{}, nil
		},
		proxy: utils.None[*ethrpc.Client](),
	}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), noRPCLimits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got common.Hash
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_sendRawTransaction", hexutil.Bytes(raw)))
	require.Equal(t, tx.Hash(), got)
	require.Equal(t, raw, broadcastRaw)

	var proofResult any
	err = client.CallContext(t.Context(), &proofResult, "eth_getProof")
	require.ErrorContains(t, err, "method eth_getProof does not exist")
	err = client.CallContext(t.Context(), nil, "status")
	require.ErrorContains(t, err, "method status does not exist")
}

func TestRejectsInvalidTransaction(t *testing.T) {
	backend := &testBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			t.Fatal("invalid transaction reached broadcaster")
			return nil, nil
		},
		proxy: utils.None[*ethrpc.Client](),
	}
	_, err := newSendAPI(backend, 1).SendRawTransaction(t.Context(), hexutil.Bytes{0x01, 0x02})
	require.Error(t, err)
}

func TestReturnsCheckTxRejection(t *testing.T) {
	_, raw := testSignedTransaction(t)
	backend := &testBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			return &coretypes.ResultBroadcastTx{Code: 1, Log: "bad nonce"}, nil
		},
		proxy: utils.None[*ethrpc.Client](),
	}
	_, err := newSendAPI(backend, 1).SendRawTransaction(t.Context(), raw)
	require.EqualError(t, err, "bad nonce")
}

func TestSendRawTransactionRefusesWhileThrottled(t *testing.T) {
	_, raw := testSignedTransaction(t)
	backend := &testBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			t.Fatal("a throttled node broadcast a transaction")
			return nil, nil
		},
		throttled: true,
	}
	_, err := newSendAPI(backend, 1).SendRawTransaction(t.Context(), raw)
	require.ErrorIs(t, err, errMempoolThrottled)
	require.Zero(t, backend.proxyCalls)
}

func TestSendRawTransactionBroadcastError(t *testing.T) {
	_, raw := testSignedTransaction(t)
	want := errors.New("mempool full")
	backend := &testBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			return nil, want
		},
		proxy: utils.None[*ethrpc.Client](),
	}

	// Test: BroadcastTx fails.
	_, err := newSendAPI(backend, 1).SendRawTransaction(t.Context(), raw)

	// Verify: that error is returned as-is.
	require.ErrorIs(t, err, want)
}

func TestSendRawTransactionMissingBroadcastResponse(t *testing.T) {
	_, raw := testSignedTransaction(t)
	backend := &testBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			return nil, nil
		},
		proxy: utils.None[*ethrpc.Client](),
	}

	// Test: BroadcastTx returns a nil result without error.
	_, err := newSendAPI(backend, 1).SendRawTransaction(t.Context(), raw)

	// Verify: treated as a missing response, not a success.
	require.EqualError(t, err, "missing broadcast response")
}

func TestSendRawTransactionRejectedWithoutLog(t *testing.T) {
	_, raw := testSignedTransaction(t)
	backend := &testBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			return &coretypes.ResultBroadcastTx{Code: 7}, nil
		},
		proxy: utils.None[*ethrpc.Client](),
	}

	// Test: CheckTx rejection with an empty log.
	_, err := newSendAPI(backend, 1).SendRawTransaction(t.Context(), raw)

	// Verify: the numeric code is used as the message.
	require.EqualError(t, err, "transaction rejected with code 7")
}

func TestSendRawTransactionSurfacesProxyError(t *testing.T) {
	_, raw := testSignedTransaction(t)
	remoteHandler := ethrpc.NewServer()
	require.NoError(t, remoteHandler.RegisterName("eth", &testRemoteSendAPI{
		sendErr: errors.New("shard owner rejected"),
	}))
	t.Cleanup(remoteHandler.Stop)
	remoteServer := httptest.NewServer(remoteHandler)
	t.Cleanup(remoteServer.Close)
	remoteClient, err := ethrpc.DialHTTP(remoteServer.URL)
	require.NoError(t, err)
	t.Cleanup(remoteClient.Close)
	backend := &testBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			t.Fatal("failed proxy reached local broadcaster")
			return nil, nil
		},
		proxy: utils.Some(remoteClient),
	}

	// Test: the shard-owner eth_sendRawTransaction call fails.
	_, err = newSendAPI(backend, 1).SendRawTransaction(t.Context(), raw)

	// Verify: that remote error is returned.
	require.ErrorContains(t, err, "shard owner rejected")
}

type testRemoteSendAPI struct {
	send    func(hexutil.Bytes) common.Hash
	sendErr error
}

func (api *testRemoteSendAPI) SendRawTransaction(input hexutil.Bytes) (common.Hash, error) {
	if api.sendErr != nil {
		return common.Hash{}, api.sendErr
	}
	return api.send(input), nil
}

func TestProxiesTransactionToShardOwner(t *testing.T) {
	tx, raw := testSignedTransaction(t)
	var proxiedRaw hexutil.Bytes
	remoteHandler := ethrpc.NewServer()
	require.NoError(t, remoteHandler.RegisterName("eth", &testRemoteSendAPI{
		send: func(input hexutil.Bytes) common.Hash {
			proxiedRaw = append(hexutil.Bytes(nil), input...)
			return tx.Hash()
		},
	}))
	t.Cleanup(remoteHandler.Stop)
	remoteServer := httptest.NewServer(remoteHandler)
	t.Cleanup(remoteServer.Close)
	remoteClient, err := ethrpc.DialHTTP(remoteServer.URL)
	require.NoError(t, err)
	t.Cleanup(remoteClient.Close)

	backend := &testBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			t.Fatal("proxied transaction reached local broadcaster")
			return nil, nil
		},
		proxy: utils.Some(remoteClient),
	}
	got, err := newSendAPI(backend, 1).SendRawTransaction(t.Context(), raw)
	require.NoError(t, err)
	require.Equal(t, tx.Hash(), got)
	require.Equal(t, hexutil.Bytes(raw), proxiedRaw)
}

func testSignedTransaction(t *testing.T) (*ethtypes.Transaction, []byte) {
	t.Helper()
	return testSignedTransactionWithNonce(t, 0)
}

// testSignedTransactionWithNonce varies the nonce so callers can get several distinct hashes.
func testSignedTransactionWithNonce(t *testing.T, nonce uint64) (*ethtypes.Transaction, []byte) {
	t.Helper()
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1_000_000_000),
		Gas:      21_000,
		To:       &to,
		Value:    big.NewInt(1),
	})
	tx, err = ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(big.NewInt(713715)), key)
	require.NoError(t, err)
	raw, err := tx.MarshalBinary()
	require.NoError(t, err)
	return tx, raw
}

func TestSkipsShardLookupWithoutProxies(t *testing.T) {
	tx, raw := testSignedTransaction(t)
	backend := &testBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			return &coretypes.ResultBroadcastTx{}, nil
		},
		proxy: utils.None[*ethrpc.Client](),
	}
	got, err := newSendAPI(backend, 1).SendRawTransaction(t.Context(), raw)
	require.NoError(t, err)
	require.Equal(t, tx.Hash(), got)
	require.Zero(t, backend.proxyCalls)
}

func TestSendConcurrencyLimitSharedByHTTPAndWebsocket(t *testing.T) {
	_, raw := testSignedTransaction(t)
	started := make(chan struct{}, 2)
	gate := make(chan struct{})
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate) }) }
	backend := &testBackend{
		broadcast: func(ctx context.Context, _ *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			started <- struct{}{}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-gate:
				return &coretypes.ResultBroadcastTx{}, nil
			}
		},
		proxy: utils.None[*ethrpc.Client](),
	}
	limits := noRPCLimits
	limits.MaxConcurrentSendCalls = 1
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), limits)
	require.NoError(t, err)
	defer handler.Stop()
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	wsServer := httptest.NewServer(websocketHandler(handler))
	defer wsServer.Close()
	defer unblock()
	httpClient, err := ethrpc.DialHTTP(httpServer.URL)
	require.NoError(t, err)
	defer httpClient.Close()
	wsClient, err := ethrpc.DialWebsocket(t.Context(), "ws"+strings.TrimPrefix(wsServer.URL, "http"), "http://example.com")
	require.NoError(t, err)
	defer wsClient.Close()
	done := make(chan error, 1)
	go func() {
		var hash common.Hash
		done <- httpClient.CallContext(t.Context(), &hash, "eth_sendRawTransaction", hexutil.Bytes(raw))
	}()
	<-started
	// Both transports and individual batch elements compete for the same slot.
	for _, client := range []*ethrpc.Client{httpClient, wsClient} {
		batch := []ethrpc.BatchElem{
			{Method: "eth_sendRawTransaction", Args: []any{hexutil.Bytes{1}}, Result: new(common.Hash)},
			{Method: "eth_sendRawTransaction", Args: []any{hexutil.Bytes{2}}, Result: new(common.Hash)},
		}
		require.NoError(t, client.BatchCallContext(t.Context(), batch))
		for _, elem := range batch {
			var rpcErr ethrpc.Error
			require.ErrorAs(t, elem.Error, &rpcErr)
			require.Equal(t, -32005, rpcErr.ErrorCode())
		}
	}
	unblock()
	require.NoError(t, <-done)
	var hash common.Hash
	require.NoError(t, wsClient.CallContext(t.Context(), &hash, "eth_sendRawTransaction", hexutil.Bytes(raw)))
}

func TestSendConcurrencySlotReleasedOnError(t *testing.T) {
	_, raw := testSignedTransaction(t)
	want := errors.New("broadcast failed")
	backend := &testBackend{
		broadcast: func(ctx context.Context, _ *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, want
		},
		proxy: utils.None[*ethrpc.Client](),
	}
	api := newSendAPI(backend, 1)
	_, err := api.SendRawTransaction(t.Context(), nil)
	require.Error(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = api.SendRawTransaction(ctx, raw)
	require.ErrorIs(t, err, context.Canceled)
	for range 2 {
		_, err = api.SendRawTransaction(t.Context(), raw)
		require.ErrorIs(t, err, want)
	}
}

func TestZeroSendConcurrencyDisablesSubmission(t *testing.T) {
	_, raw := testSignedTransaction(t)
	var broadcasts atomic.Int64
	backend := &testBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			broadcasts.Add(1)
			return &coretypes.ResultBroadcastTx{}, nil
		},
		proxy: utils.None[*ethrpc.Client](),
	}
	limits := noRPCLimits
	limits.MaxConcurrentSendCalls = 0
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), limits)
	require.NoError(t, err)
	defer handler.Stop()
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	wsServer := httptest.NewServer(websocketHandler(handler))
	defer wsServer.Close()
	httpClient, err := ethrpc.DialHTTP(httpServer.URL)
	require.NoError(t, err)
	defer httpClient.Close()
	wsClient, err := ethrpc.DialWebsocket(t.Context(), "ws"+strings.TrimPrefix(wsServer.URL, "http"), "http://example.com")
	require.NoError(t, err)
	defer wsClient.Close()
	for _, client := range []*ethrpc.Client{httpClient, wsClient} {
		for _, input := range []hexutil.Bytes{raw, {1}} {
			var hash common.Hash
			err := client.CallContext(t.Context(), &hash, "eth_sendRawTransaction", input)
			var rpcErr ethrpc.Error
			require.ErrorAs(t, err, &rpcErr)
			require.Equal(t, -32005, rpcErr.ErrorCode())
		}
	}
	require.Zero(t, broadcasts.Load())
}
