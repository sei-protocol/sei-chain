package rpc

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

func TestGetBalance(t *testing.T) {
	address := common.HexToAddress("0x1000000000000000000000000000000000000001")
	want := uint256.NewInt(123456789)
	backend := &testBackend{
		balance: func(got common.Address) uint256.Int {
			require.Equal(t, address, got)
			return *want
		},
	}
	api := &stateAPI{backend: backend}

	for _, tag := range []ethrpc.BlockNumber{
		ethrpc.LatestBlockNumber,
		ethrpc.SafeBlockNumber,
		ethrpc.FinalizedBlockNumber,
		ethrpc.PendingBlockNumber,
	} {
		got, err := api.GetBalance(t.Context(), address, ethrpc.BlockNumberOrHashWithNumber(tag))
		require.NoError(t, err)
		require.Equal(t, want.ToBig(), got.ToInt())
	}
}

func TestGetBalanceRejectsHistoricalState(t *testing.T) {
	backend := &testBackend{
		balance: func(common.Address) uint256.Int {
			t.Fatal("historical request read the current balance")
			return uint256.Int{}
		},
	}
	api := &stateAPI{backend: backend}
	address := common.Address{1}

	for _, block := range []ethrpc.BlockNumberOrHash{
		ethrpc.BlockNumberOrHashWithNumber(ethrpc.EarliestBlockNumber),
		ethrpc.BlockNumberOrHashWithNumber(7),
		ethrpc.BlockNumberOrHashWithHash(common.Hash{2}, true),
		{},
	} {
		got, err := api.GetBalance(t.Context(), address, block)
		require.ErrorIs(t, err, errHistoricalStateUnsupported)
		require.Nil(t, got)
	}
}

func TestHandlerServesGetBalance(t *testing.T) {
	address := common.HexToAddress("0x2000000000000000000000000000000000000002")
	backend := &testBackend{
		balance: func(common.Address) uint256.Int {
			return *uint256.NewInt(42)
		},
	}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore())
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got hexutil.Big
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getBalance", address, "latest"))
	require.Equal(t, "0x2a", got.String())
}

func TestGetCode(t *testing.T) {
	address := common.HexToAddress("0x3000000000000000000000000000000000000003")
	want := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}
	backend := &testBackend{
		code: func(got common.Address) ([]byte, error) {
			require.Equal(t, address, got)
			return want, nil
		},
	}
	api := &stateAPI{backend: backend}

	for _, tag := range []ethrpc.BlockNumber{
		ethrpc.LatestBlockNumber,
		ethrpc.SafeBlockNumber,
		ethrpc.FinalizedBlockNumber,
		ethrpc.PendingBlockNumber,
	} {
		got, err := api.GetCode(t.Context(), address, ethrpc.BlockNumberOrHashWithNumber(tag))
		require.NoError(t, err)
		require.Equal(t, hexutil.Bytes(want), got)
	}
}

func TestGetCodeRejectsHistoricalState(t *testing.T) {
	backend := &testBackend{
		code: func(common.Address) ([]byte, error) {
			t.Fatal("historical request read the current code")
			return nil, nil
		},
	}
	api := &stateAPI{backend: backend}
	address := common.Address{1}

	for _, block := range []ethrpc.BlockNumberOrHash{
		ethrpc.BlockNumberOrHashWithNumber(ethrpc.EarliestBlockNumber),
		ethrpc.BlockNumberOrHashWithNumber(7),
		ethrpc.BlockNumberOrHashWithHash(common.Hash{2}, true),
		{},
	} {
		got, err := api.GetCode(t.Context(), address, block)
		require.ErrorIs(t, err, errHistoricalStateUnsupported)
		require.Nil(t, got)
	}
}

func TestGetCodeReturnsBackendError(t *testing.T) {
	want := errors.New("no code reader")
	api := &stateAPI{backend: &testBackend{
		code: func(common.Address) ([]byte, error) { return nil, want },
	}}

	got, err := api.GetCode(t.Context(), common.Address{1}, ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber))

	require.ErrorIs(t, err, want)
	require.Nil(t, got)
}

func TestHandlerServesGetCode(t *testing.T) {
	contract := common.HexToAddress("0x4000000000000000000000000000000000000004")
	backend := &testBackend{
		code: func(address common.Address) ([]byte, error) {
			if address == contract {
				return []byte{0xfe}, nil
			}
			return nil, nil
		},
	}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore())
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got hexutil.Bytes
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getCode", contract, "latest"))
	require.Equal(t, "0xfe", got.String())

	// An account without code answers "0x", matching geth, rather than null.
	var raw json.RawMessage
	require.NoError(t, client.CallContext(t.Context(), &raw, "eth_getCode", common.Address{9}, "latest"))
	require.JSONEq(t, `"0x"`, string(raw))

	err = client.CallContext(t.Context(), &got, "eth_getCode", contract, "0x1")
	require.ErrorContains(t, err, errHistoricalStateUnsupported.Error())
}
