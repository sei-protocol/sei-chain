package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
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

// A numbered block, the head's own included, is read at that block rather than from the latest state.
func TestGetBalanceReadsNumberedBlocksFromHistory(t *testing.T) {
	address := common.Address{1}
	api := &stateAPI{backend: &testBackend{
		blockNumber: func() uint64 { return 9 },
		balance: func(common.Address) uint256.Int {
			t.Fatal("a numbered block read the latest state")
			return uint256.Int{}
		},
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
	_, err = api.GetBalance(t.Context(), address, ethrpc.BlockNumberOrHashWithNumber(-6))
	require.ErrorIs(t, err, errBlockTagUnsupported)
}

// pastBlocksBackend returns a backend at head whose block store holds blocks 1 to head+1, the last not
// executed yet, each hashed by traceBlockHash.
func pastBlocksBackend(head int64) *testBackend {
	return &testBackend{
		block: func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			height := head
			if req.Height != nil {
				height = int64(*req.Height)
			}
			if height <= 0 {
				return nil, coretypes.ErrZeroOrNegativeHeight
			}
			if height > head+1 {
				return nil, coretypes.ErrHeightExceedsChainHead
			}
			return traceResultBlock(height, nil), nil
		},
		blockByHash: func(_ context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
			for height := int64(1); height <= head+1; height++ {
				if traceBlockHash(height) == common.BytesToHash(req.Hash) {
					return traceResultBlock(height, nil), nil
				}
			}
			return nil, nil
		},
		blockNumber: func() uint64 { return uint64(head) }, //nolint:gosec // G115: test heights are positive.
		gasLimit:    func() (uint64, error) { return traceGasLimit, nil },
		baseFee:     func() (*big.Int, error) { return new(big.Int), nil },
		chainID:     func() uint64 { return traceChainID },
	}
}

// Every state query resolves its block here: a latest tag to the latest state, and a number or hash
// to a block no higher than the head.
func TestStateHeight(t *testing.T) {
	backend := pastBlocksBackend(5)
	backendErr := errors.New("block store unavailable")

	for _, tc := range []struct {
		name       string
		block      ethrpc.BlockNumberOrHash
		byHashErr  error
		wantHeight int64
		wantLatest bool
		wantErr    error
	}{
		{name: "latest", block: ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber), wantLatest: true},
		{name: "safe", block: ethrpc.BlockNumberOrHashWithNumber(ethrpc.SafeBlockNumber), wantLatest: true},
		{name: "finalized", block: ethrpc.BlockNumberOrHashWithNumber(ethrpc.FinalizedBlockNumber), wantLatest: true},
		{name: "pending", block: ethrpc.BlockNumberOrHashWithNumber(ethrpc.PendingBlockNumber), wantLatest: true},
		{name: "earliest", block: ethrpc.BlockNumberOrHashWithNumber(ethrpc.EarliestBlockNumber), wantHeight: 0},
		{name: "past number", block: ethrpc.BlockNumberOrHashWithNumber(3), wantHeight: 3},
		{name: "head number", block: ethrpc.BlockNumberOrHashWithNumber(5), wantHeight: 5},
		{name: "canonical hash", block: ethrpc.BlockNumberOrHashWithHash(traceBlockHash(3), true), wantHeight: 3},
		{name: "hash", block: ethrpc.BlockNumberOrHashWithHash(traceBlockHash(3), false), wantHeight: 3},
		{name: "head hash", block: ethrpc.BlockNumberOrHashWithHash(traceBlockHash(5), true), wantHeight: 5},
		{name: "unknown hash", block: ethrpc.BlockNumberOrHashWithHash(common.Hash{0xaa}, false), wantErr: errBlockNotFound},
		{name: "hash of a block not executed yet", block: ethrpc.BlockNumberOrHashWithHash(traceBlockHash(6), false), wantErr: errBlockAboveHead},
		{name: "hash lookup fails", block: ethrpc.BlockNumberOrHashWithHash(traceBlockHash(3), false), byHashErr: backendErr, wantErr: backendErr},
		{name: "number above head", block: ethrpc.BlockNumberOrHashWithNumber(6), wantErr: errBlockAboveHead},
		{name: "unknown tag", block: ethrpc.BlockNumberOrHashWithNumber(-6), wantErr: errBlockTagUnsupported},
		{name: "neither number nor hash", wantErr: errBlockNotSpecified},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := *backend
			if tc.byHashErr != nil {
				backend.blockByHash = func(context.Context, *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
					return nil, tc.byHashErr
				}
			}

			height, latest, err := stateHeight(t.Context(), &backend, tc.block)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantLatest, latest)
			require.Equal(t, tc.wantHeight, height)
		})
	}
}

// Every method reading state answers a block named by hash, as EIP-1898 encodes it, from the state
// after that block.
func TestHandlerServesStateAtABlockHash(t *testing.T) {
	address := common.HexToAddress("0x7000000000000000000000000000000000000007")
	backend := pastBlocksBackend(5)
	wantHeight := func(height int64) {
		require.Equal(t, int64(3), height)
	}
	wantHeader := func(header *ethtypes.Header) {
		require.Equal(t, int64(3), header.Number.Int64())
		require.Equal(t, traceBlockHash(2), header.ParentHash)
	}
	backend.balanceAt = func(_ common.Address, height int64) (uint256.Int, error) {
		wantHeight(height)
		return *uint256.NewInt(0x30), nil
	}
	backend.codeAt = func(_ common.Address, height int64) ([]byte, error) {
		wantHeight(height)
		return []byte{0x31}, nil
	}
	backend.storageAt = func(_ common.Address, _ common.Hash, height int64) (common.Hash, error) {
		wantHeight(height)
		return common.Hash{0x32}, nil
	}
	backend.transactionCountAt = func(_ common.Address, height int64) (uint64, error) {
		wantHeight(height)
		return 0x33, nil
	}
	backend.callAt = func(_ context.Context, _ *core.Message, header *ethtypes.Header) (*core.ExecutionResult, error) {
		wantHeader(header)
		return &core.ExecutionResult{ReturnData: []byte{0x34}}, nil
	}
	backend.estimateGasAt = func(_ context.Context, _ *core.Message, _ uint64, header *ethtypes.Header) (uint64, []byte, error) {
		wantHeader(header)
		return 0x35, nil, nil
	}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), noRPCLimits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	block := map[string]any{"blockHash": traceBlockHash(3), "requireCanonical": true}
	call := map[string]any{"to": address}

	for _, tc := range []struct {
		method string
		params []any
		want   string
	}{
		{method: "eth_getBalance", params: []any{address, block}, want: `"0x30"`},
		{method: "eth_getCode", params: []any{address, block}, want: `"0x31"`},
		{method: "eth_getStorageAt", params: []any{address, "0x0", block}, want: `"0x` + common.Hash{0x32}.Hex()[2:] + `"`},
		{method: "eth_getTransactionCount", params: []any{address, block}, want: `"0x33"`},
		{method: "eth_call", params: []any{call, block}, want: `"0x34"`},
		{method: "eth_estimateGas", params: []any{call, block}, want: `"0x35"`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			var got json.RawMessage
			require.NoError(t, client.CallContext(t.Context(), &got, tc.method, tc.params...))
			require.JSONEq(t, tc.want, string(got))

			unknown := map[string]any{"blockHash": common.Hash{0xaa}}
			params := append(append([]any{}, tc.params[:len(tc.params)-1]...), unknown)
			require.ErrorContains(t, client.CallContext(t.Context(), &got, tc.method, params...), errBlockNotFound.Error())
		})
	}
}

func TestHandlerServesGetBalance(t *testing.T) {
	address := common.HexToAddress("0x2000000000000000000000000000000000000002")
	backend := &testBackend{
		balance: func(common.Address) uint256.Int {
			return *uint256.NewInt(42)
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
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), noRPCLimits)
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
		// A missing block selector, malformed keys, and backend failures are errors.
		{name: "neither number nor hash", key: "0x1", wantErr: errBlockNotSpecified},
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

// A numbered block, the head's own included, is read at that block rather than from the latest state.
func TestGetStorageAtReadsNumberedBlocksFromHistory(t *testing.T) {
	address := common.Address{1}
	slot := common.BigToHash(big.NewInt(1))
	api := &stateAPI{backend: &testBackend{
		blockNumber: func() uint64 { return 9 },
		storage: func(common.Address, common.Hash) (common.Hash, error) {
			t.Fatal("a numbered block read the latest state")
			return common.Hash{}, nil
		},
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
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore(), noRPCLimits)
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
