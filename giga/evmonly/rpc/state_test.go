package rpc

import (
	"encoding/json"
	"errors"
	"math/big"
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

func TestGetStorageAt(t *testing.T) {
	address := common.HexToAddress("0x5000000000000000000000000000000000000005")
	value := common.BigToHash(big.NewInt(0x2a))
	latest := ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber)
	backendErr := errors.New("no storage reader")

	for _, tc := range []struct {
		name       string
		key        string
		block      ethrpc.BlockNumberOrHash
		backendErr error
		wantKey    common.Hash
		wantErr    error
	}{
		// Current-state tags all read the current committed state.
		{name: "latest", key: "0x1", block: latest, wantKey: common.BigToHash(big.NewInt(1))},
		{name: "safe", key: "0x1", block: ethrpc.BlockNumberOrHashWithNumber(ethrpc.SafeBlockNumber), wantKey: common.BigToHash(big.NewInt(1))},
		{name: "finalized", key: "0x1", block: ethrpc.BlockNumberOrHashWithNumber(ethrpc.FinalizedBlockNumber), wantKey: common.BigToHash(big.NewInt(1))},
		{name: "pending", key: "0x1", block: ethrpc.BlockNumberOrHashWithNumber(ethrpc.PendingBlockNumber), wantKey: common.BigToHash(big.NewInt(1))},
		// Keys decode as go-ethereum does: optional prefix, odd length, left-padded to 32 bytes.
		{name: "full 32-byte key", key: common.HexToHash("0xabcd").Hex(), block: latest, wantKey: common.HexToHash("0xabcd")},
		{name: "odd-length key", key: "0x123", block: latest, wantKey: common.BigToHash(big.NewInt(0x123))},
		{name: "unprefixed key", key: "ff", block: latest, wantKey: common.BigToHash(big.NewInt(0xff))},
		{name: "uppercase prefix", key: "0X1", block: latest, wantKey: common.BigToHash(big.NewInt(1))},
		{name: "empty key is slot zero", key: "0x", block: latest},
		// Historical state, malformed keys, and backend failures are errors.
		{name: "earliest", key: "0x1", block: ethrpc.BlockNumberOrHashWithNumber(ethrpc.EarliestBlockNumber), wantErr: errHistoricalStateUnsupported},
		{name: "explicit height", key: "0x1", block: ethrpc.BlockNumberOrHashWithNumber(7), wantErr: errHistoricalStateUnsupported},
		{name: "block hash", key: "0x1", block: ethrpc.BlockNumberOrHashWithHash(common.Hash{2}, true), wantErr: errHistoricalStateUnsupported},
		{name: "neither number nor hash", key: "0x1", wantErr: errHistoricalStateUnsupported},
		{name: "non-hex key", key: "0xzz", block: latest, wantErr: errStorageKeyInvalid},
		{name: "doubled prefix", key: "0x0X1", block: latest, wantErr: errStorageKeyInvalid},
		{name: "key longer than 32 bytes", key: "0x01" + common.Hash{}.Hex()[2:], block: latest, wantErr: errStorageKeyTooLong},
		{name: "backend error", key: "0x1", block: latest, backendErr: backendErr, wantKey: common.BigToHash(big.NewInt(1)), wantErr: backendErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := false
			api := &stateAPI{backend: &testBackend{
				storage: func(gotAddress common.Address, gotKey common.Hash) (common.Hash, error) {
					read = true
					require.Equal(t, address, gotAddress)
					require.Equal(t, tc.wantKey, gotKey)
					return value, tc.backendErr
				},
			}}

			got, err := api.GetStorageAt(t.Context(), address, tc.key, tc.block)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Nil(t, got)
				require.Equal(t, tc.backendErr != nil, read)
				return
			}
			require.NoError(t, err)
			require.True(t, read)
			require.Equal(t, hexutil.Bytes(value[:]), got)
		})
	}
}

func TestHandlerServesGetStorageAt(t *testing.T) {
	contract := common.HexToAddress("0x6000000000000000000000000000000000000006")
	slot := common.BigToHash(big.NewInt(1))
	backend := &testBackend{
		storage: func(address common.Address, key common.Hash) (common.Hash, error) {
			if address == contract && key == slot {
				return common.BigToHash(big.NewInt(0x2a)), nil
			}
			return common.Hash{}, nil
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
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getStorageAt", contract, "0x1", "latest"))
	require.Equal(t, common.BigToHash(big.NewInt(0x2a)).Hex(), got.String())

	// An unset slot answers 32 zero bytes, matching geth, rather than null or "0x".
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getStorageAt", contract, "0x2", "latest"))
	require.Equal(t, common.Hash{}.Hex(), got.String())

	err = client.CallContext(t.Context(), &got, "eth_getStorageAt", contract, "0x1", "0x1")
	require.ErrorContains(t, err, errHistoricalStateUnsupported.Error())
}
