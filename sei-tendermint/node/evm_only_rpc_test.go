package node

import (
	"context"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

type testEVMOnlyRPCBackend struct {
	broadcast func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error)
	proxy     utils.Option[*ethrpc.Client]
}

func (b *testEVMOnlyRPCBackend) BroadcastTx(ctx context.Context, req *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
	return b.broadcast(ctx, req)
}

func (b *testEVMOnlyRPCBackend) EvmProxy(common.Address) utils.Option[*ethrpc.Client] {
	return b.proxy
}

func TestEVMOnlyRPCSendRawTransaction(t *testing.T) {
	tx, raw := testEVMOnlySignedTransaction(t)
	var broadcastRaw []byte
	backend := &testEVMOnlyRPCBackend{
		broadcast: func(_ context.Context, req *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			broadcastRaw = append([]byte(nil), req.Tx...)
			return &coretypes.ResultBroadcastTx{}, nil
		},
		proxy: utils.None[*ethrpc.Client](),
	}
	handler, err := newEVMOnlyRPCHandler(backend)
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

	var chainID hexutil.Big
	err = client.CallContext(t.Context(), &chainID, "eth_chainId")
	require.ErrorContains(t, err, "method eth_chainId does not exist")
	err = client.CallContext(t.Context(), nil, "status")
	require.ErrorContains(t, err, "method status does not exist")
}

func TestEVMOnlyRPCRejectsInvalidTransaction(t *testing.T) {
	backend := &testEVMOnlyRPCBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			t.Fatal("invalid transaction reached broadcaster")
			return nil, nil
		},
		proxy: utils.None[*ethrpc.Client](),
	}
	_, err := (&evmOnlySendAPI{backend: backend}).SendRawTransaction(t.Context(), hexutil.Bytes{0x01, 0x02})
	require.Error(t, err)
}

func TestEVMOnlyRPCReturnsCheckTxRejection(t *testing.T) {
	_, raw := testEVMOnlySignedTransaction(t)
	backend := &testEVMOnlyRPCBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			return &coretypes.ResultBroadcastTx{Code: 1, Log: "bad nonce"}, nil
		},
		proxy: utils.None[*ethrpc.Client](),
	}
	_, err := (&evmOnlySendAPI{backend: backend}).SendRawTransaction(t.Context(), raw)
	require.EqualError(t, err, "bad nonce")
}

func TestEVMOnlyRPCProxiesTransactionToShardOwner(t *testing.T) {
	tx, raw := testEVMOnlySignedTransaction(t)
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

	backend := &testEVMOnlyRPCBackend{
		broadcast: func(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error) {
			t.Fatal("proxied transaction reached local broadcaster")
			return nil, nil
		},
		proxy: utils.Some(remoteClient),
	}
	got, err := (&evmOnlySendAPI{backend: backend}).SendRawTransaction(t.Context(), raw)
	require.NoError(t, err)
	require.Equal(t, tx.Hash(), got)
	require.Equal(t, hexutil.Bytes(raw), proxiedRaw)
}

type testRemoteSendAPI struct {
	send func(hexutil.Bytes) common.Hash
}

func (api *testRemoteSendAPI) SendRawTransaction(input hexutil.Bytes) common.Hash {
	return api.send(input)
}

func testEVMOnlySignedTransaction(t *testing.T) (*ethtypes.Transaction, []byte) {
	t.Helper()
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1_000_000_000),
		Gas:      21_000,
		To:       &to,
		Value:    big.NewInt(1),
	})
	tx, err = ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(big.NewInt(1337)), key)
	require.NoError(t, err)
	raw, err := tx.MarshalBinary()
	require.NoError(t, err)
	return tx, raw
}
