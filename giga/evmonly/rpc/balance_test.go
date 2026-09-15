package rpc

import (
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
	api := &balanceAPI{backend: backend}

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
	api := &balanceAPI{backend: backend}
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
