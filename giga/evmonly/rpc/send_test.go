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
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
)

func TestSendRawTransaction(t *testing.T) {
	_, raw := testSignedTransaction(t)
	returnedHash := common.HexToHash("0x1234")
	var broadcastRaw []byte
	backend := &testBackend{
		insertTx: func(_ context.Context, req abevm.RawTx) (common.Hash, error) {
			broadcastRaw = append([]byte(nil), req...)
			return returnedHash, nil
		},
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
	require.Equal(t, returnedHash, got)
	require.Equal(t, raw, broadcastRaw)

	var accounts any
	err = client.CallContext(t.Context(), &accounts, "eth_accounts")
	require.ErrorContains(t, err, "method eth_accounts does not exist")
	err = client.CallContext(t.Context(), nil, "status")
	require.ErrorContains(t, err, "method status does not exist")
}

func TestReturnsCheckTxRejection(t *testing.T) {
	_, raw := testSignedTransaction(t)
	backend := &testBackend{
		insertTx: func(context.Context, abevm.RawTx) (common.Hash, error) {
			return common.Hash{}, errors.New("bad nonce")
		},
	}
	_, err := newSendAPI(backend, 1).SendRawTransaction(t.Context(), raw)
	require.EqualError(t, err, "bad nonce")
}

func TestSendRawTransactionWrongChainID(t *testing.T) {
	chainID := big.NewInt(713700)
	wrongChainID := big.NewInt(74565)
	signer := abevm.NewSigner(ethtypes.LatestSignerForChainID(chainID), 0, 1)
	backend := &testBackend{
		insertTx: func(_ context.Context, raw abevm.RawTx) (common.Hash, error) {
			_, err := signer.ParseTx(raw)
			require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
			return common.Hash{}, err
		},
	}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), noRPCLimits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	for _, txData := range []ethtypes.TxData{
		&ethtypes.LegacyTx{To: &to, Gas: 21000, GasPrice: big.NewInt(1_000_000_000)},
		&ethtypes.DynamicFeeTx{ChainID: wrongChainID, To: &to, Gas: 21000, GasFeeCap: big.NewInt(1_000_000_000)},
	} {
		tx, err := ethtypes.SignTx(ethtypes.NewTx(txData), ethtypes.LatestSignerForChainID(wrongChainID), key)
		require.NoError(t, err)
		raw, err := tx.MarshalBinary()
		require.NoError(t, err)
		var hash common.Hash
		err = client.CallContext(t.Context(), &hash, "eth_sendRawTransaction", hexutil.Bytes(raw))
		require.EqualError(t, err, "invalid chain-id")
		var rpcErr ethrpc.Error
		require.ErrorAs(t, err, &rpcErr)
		require.Equal(t, -32000, rpcErr.ErrorCode())
	}
}

func TestSendRawTransactionBroadcastError(t *testing.T) {
	_, raw := testSignedTransaction(t)
	want := errors.New("mempool full")
	backend := &testBackend{
		insertTx: func(context.Context, abevm.RawTx) (common.Hash, error) {
			return common.Hash{}, want
		},
	}

	// Test: InsertTx fails.
	_, err := newSendAPI(backend, 1).SendRawTransaction(t.Context(), raw)

	// Verify: that error is returned as-is.
	require.ErrorIs(t, err, want)
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
	tx, err = ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(big.NewInt(713700)), key)
	require.NoError(t, err)
	raw, err := tx.MarshalBinary()
	require.NoError(t, err)
	return tx, raw
}

func TestSendConcurrencyLimitSharedByHTTPAndWebsocket(t *testing.T) {
	_, raw := testSignedTransaction(t)
	started := make(chan struct{}, 2)
	gate := make(chan struct{})
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate) }) }
	backend := &testBackend{
		insertTx: func(ctx context.Context, _ abevm.RawTx) (common.Hash, error) {
			started <- struct{}{}
			select {
			case <-ctx.Done():
				return common.Hash{}, ctx.Err()
			case <-gate:
				return common.Hash{}, nil
			}
		},
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
		insertTx: func(ctx context.Context, _ abevm.RawTx) (common.Hash, error) {
			if err := ctx.Err(); err != nil {
				return common.Hash{}, err
			}
			return common.Hash{}, want
		},
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
		insertTx: func(context.Context, abevm.RawTx) (common.Hash, error) {
			broadcasts.Add(1)
			return common.Hash{}, nil
		},
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
