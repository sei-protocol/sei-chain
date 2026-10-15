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

// A numbered block below the head is read from history; the head's own number is the latest state.
func TestGetBalanceReadsNumberedBlocksFromHistory(t *testing.T) {
	address := common.Address{1}
	api := &stateAPI{backend: &testBackend{
		blockNumber: func() uint64 { return 9 },
		balance:     func(common.Address) uint256.Int { return *uint256.NewInt(900) },
		balanceAt: func(got common.Address, height int64) (uint256.Int, error) {
			require.Equal(t, address, got)
			return *uint256.NewInt(uint64(height) * 100), nil //nolint:gosec // test heights are small
		},
	}}

	for height, want := range map[ethrpc.BlockNumber]int64{ethrpc.EarliestBlockNumber: 0, 7: 700, 9: 900} {
		got, err := api.GetBalance(t.Context(), address, ethrpc.BlockNumberOrHashWithNumber(height))
		require.NoError(t, err)
		require.Equal(t, want, got.ToInt().Int64(), "block %d", height)
	}
	_, err := api.GetBalance(t.Context(), address, ethrpc.BlockNumberOrHashWithNumber(10))
	require.ErrorIs(t, err, errBlockAboveHead)
}

func TestGetBalanceRejectsABlockHash(t *testing.T) {
	api := &stateAPI{backend: &testBackend{}}
	for _, block := range []ethrpc.BlockNumberOrHash{ethrpc.BlockNumberOrHashWithHash(common.Hash{2}, true), {}} {
		got, err := api.GetBalance(t.Context(), common.Address{1}, block)
		require.ErrorIs(t, err, errBlockHashUnsupported)
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

func TestGetCodeReadsNumberedBlocksFromHistory(t *testing.T) {
	address := common.Address{1}
	api := &stateAPI{backend: &testBackend{
		blockNumber: func() uint64 { return 9 },
		codeAt: func(got common.Address, height int64) ([]byte, error) {
			require.Equal(t, address, got)
			require.Equal(t, int64(7), height)
			return []byte{0xfe}, nil
		},
	}}

	got, err := api.GetCode(t.Context(), address, ethrpc.BlockNumberOrHashWithNumber(7))
	require.NoError(t, err)
	require.Equal(t, hexutil.Bytes{0xfe}, got)

	for _, block := range []ethrpc.BlockNumberOrHash{ethrpc.BlockNumberOrHashWithHash(common.Hash{2}, true), {}} {
		got, err := api.GetCode(t.Context(), address, block)
		require.ErrorIs(t, err, errBlockHashUnsupported)
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
		blockNumber: func() uint64 { return 5 },
		codeAt:      func(common.Address, int64) ([]byte, error) { return nil, nil },
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

	// Before the contract was deployed.
	require.NoError(t, client.CallContext(t.Context(), &raw, "eth_getCode", contract, "0x1"))
	require.JSONEq(t, `"0x"`, string(raw))
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
		// Block hashes, malformed keys, and backend failures are errors.
		{name: "block hash", key: "0x1", block: ethrpc.BlockNumberOrHashWithHash(common.Hash{2}, true), wantErr: errBlockHashUnsupported},
		{name: "neither number nor hash", key: "0x1", wantErr: errBlockHashUnsupported},
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

// A numbered block below the head is read from history; the head's own number is the latest state.
func TestGetStorageAtReadsNumberedBlocksFromHistory(t *testing.T) {
	address := common.Address{1}
	slot := common.BigToHash(big.NewInt(1))
	api := &stateAPI{backend: &testBackend{
		blockNumber: func() uint64 { return 9 },
		storage:     func(common.Address, common.Hash) (common.Hash, error) { return common.Hash{9}, nil },
		storageAt: func(gotAddress common.Address, gotKey common.Hash, height int64) (common.Hash, error) {
			require.Equal(t, address, gotAddress)
			require.Equal(t, slot, gotKey)
			return common.Hash{byte(height)}, nil //nolint:gosec // test heights are small
		},
	}}

	for number, want := range map[ethrpc.BlockNumber]common.Hash{ethrpc.EarliestBlockNumber: {0}, 7: {7}, 9: {9}} {
		got, err := api.GetStorageAt(t.Context(), address, "0x1", ethrpc.BlockNumberOrHashWithNumber(number))
		require.NoError(t, err)
		require.Equal(t, hexutil.Bytes(want[:]), got, "block %d", number)
	}
	_, err := api.GetStorageAt(t.Context(), address, "0x1", ethrpc.BlockNumberOrHashWithNumber(10))
	require.ErrorIs(t, err, errBlockAboveHead)
}

func TestHandlerServesGetStorageAt(t *testing.T) {
	contract := common.HexToAddress("0x6000000000000000000000000000000000000006")
	slot := common.BigToHash(big.NewInt(1))
	backend := &testBackend{
		blockNumber: func() uint64 { return 5 },
		storage: func(address common.Address, key common.Hash) (common.Hash, error) {
			if address == contract && key == slot {
				return common.BigToHash(big.NewInt(0x2a)), nil
			}
			return common.Hash{}, nil
		},
		storageAt: func(address common.Address, key common.Hash, height int64) (common.Hash, error) {
			if address == contract && key == slot && height == 3 {
				return common.BigToHash(big.NewInt(0x11)), nil
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

	// A numbered block below the head reads that block's history.
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getStorageAt", contract, "0x1", "0x3"))
	require.Equal(t, common.BigToHash(big.NewInt(0x11)).Hex(), got.String())
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getStorageAt", contract, "0x1", "0x2"))
	require.Equal(t, common.Hash{}.Hex(), got.String())
}
