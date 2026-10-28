package rpc

import (
	"context"
	"errors"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/export"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

func latestBlockSelector() *ethrpc.BlockNumberOrHash {
	block := ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber)
	return &block
}

func TestEstimateGasHappyPath(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	from := common.HexToAddress("0x2000000000000000000000000000000000000002")
	var gotMsg *core.Message
	var gotGasCap uint64
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		baseFee: func() (*big.Int, error) { return new(big.Int), nil },
		estimateGas: func(_ context.Context, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
			gotMsg = msg
			gotGasCap = gasCap
			return 21_000, nil, nil
		},
	}
	api := &estimateAPI{backend: backend}

	got, err := api.EstimateGas(t.Context(), export.TransactionArgs{From: &from, To: &to}, latestBlockSelector(), nil, nil)

	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(21_000), got)
	require.NotNil(t, gotMsg)
	require.Equal(t, from, gotMsg.From)
	require.Equal(t, &to, gotMsg.To)
	require.True(t, gotMsg.SkipNonceChecks)
	require.True(t, gotMsg.SkipFromEOACheck)
	require.Equal(t, uint64(defaultCallGasCap), gotGasCap)
}

func TestEstimateGasOmittedGasUsesGasEstimatorDefault(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	var gotMsg *core.Message
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		baseFee: func() (*big.Int, error) { return new(big.Int), nil },
		estimateGas: func(_ context.Context, msg *core.Message, _ uint64) (uint64, []byte, error) {
			gotMsg = msg
			return 21_000, nil, nil
		},
	}
	api := &estimateAPI{backend: backend}

	_, err := api.EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latestBlockSelector(), nil, nil)

	require.NoError(t, err)
	require.NotNil(t, gotMsg)
	// An omitted gas must reach the backend as 0, not defaultCallGasCap.
	require.Zero(t, gotMsg.GasLimit)
}

func TestEstimateGasPreservesExplicitGas(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	var gotMsg *core.Message
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		baseFee: func() (*big.Int, error) { return new(big.Int), nil },
		estimateGas: func(_ context.Context, msg *core.Message, _ uint64) (uint64, []byte, error) {
			gotMsg = msg
			return 21_000, nil, nil
		},
	}
	api := &estimateAPI{backend: backend}
	explicit := hexutil.Uint64(50_000)

	_, err := api.EstimateGas(t.Context(), export.TransactionArgs{To: &to, Gas: &explicit}, latestBlockSelector(), nil, nil)

	require.NoError(t, err)
	require.Equal(t, uint64(50_000), gotMsg.GasLimit)
}

func TestEstimateGasCapsExplicitGasAboveDefault(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	var gotMsg *core.Message
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		baseFee: func() (*big.Int, error) { return new(big.Int), nil },
		estimateGas: func(_ context.Context, msg *core.Message, _ uint64) (uint64, []byte, error) {
			gotMsg = msg
			return 21_000, nil, nil
		},
	}
	api := &estimateAPI{backend: backend}
	explicit := hexutil.Uint64(defaultCallGasCap * 10)

	_, err := api.EstimateGas(t.Context(), export.TransactionArgs{To: &to, Gas: &explicit}, latestBlockSelector(), nil, nil)

	require.NoError(t, err)
	require.Equal(t, uint64(defaultCallGasCap), gotMsg.GasLimit)
}

func TestEstimateGasSurfacesRevertReason(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	revert := revertData("insufficient balance")
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		baseFee: func() (*big.Int, error) { return new(big.Int), nil },
		estimateGas: func(context.Context, *core.Message, uint64) (uint64, []byte, error) {
			return 0, revert, vm.ErrExecutionReverted
		},
	}
	api := &estimateAPI{backend: backend}

	got, err := api.EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latestBlockSelector(), nil, nil)

	require.Zero(t, got)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient balance")
	var revertErr *revertError
	require.ErrorAs(t, err, &revertErr)
	require.Equal(t, 3, revertErr.ErrorCode())
	require.Equal(t, hexutil.Encode(revert), revertErr.ErrorData())
}

func TestEstimateGasPassesThroughNonRevertError(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	wantErr := errors.New("gas required exceeds allowance (40000)")
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		baseFee: func() (*big.Int, error) { return new(big.Int), nil },
		estimateGas: func(context.Context, *core.Message, uint64) (uint64, []byte, error) {
			return 0, nil, wantErr
		},
	}
	api := &estimateAPI{backend: backend}

	got, err := api.EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latestBlockSelector(), nil, nil)

	require.Zero(t, got)
	require.ErrorIs(t, err, wantErr)
}

// A past block's estimate runs against the state after that block, in the context it executed at.
func TestEstimateGasRunsAgainstAPastBlock(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	for _, tc := range pastBlockSelectors {
		t.Run(tc.name, func(t *testing.T) {
			backend := pastBlocksBackend(5)
			backend.estimateGas = func(context.Context, *core.Message, uint64) (uint64, []byte, error) {
				t.Fatal("a past block's estimate ran against the latest state")
				return 0, nil, nil
			}
			backend.estimateGasAt = func(_ context.Context, msg *core.Message, gasCap uint64, header *ethtypes.Header) (uint64, []byte, error) {
				require.Equal(t, &to, msg.To)
				require.Equal(t, uint64(defaultCallGasCap), gasCap)
				requirePastHeader(t, tc.wantNumber, header)
				return 30_000, nil, nil
			}

			got, err := (&estimateAPI{backend: backend, blocks: &blockAPI{backend: backend}}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, &tc.block, nil, nil)

			require.NoError(t, err)
			require.Equal(t, hexutil.Uint64(30_000), got)
		})
	}

	t.Run("revert", func(t *testing.T) {
		backend := pastBlocksBackend(5)
		backend.estimateGasAt = func(context.Context, *core.Message, uint64, *ethtypes.Header) (uint64, []byte, error) {
			return 0, revertData("too early"), vm.ErrExecutionReverted
		}
		block := ethrpc.BlockNumberOrHashWithNumber(3)

		_, err := (&estimateAPI{backend: backend, blocks: &blockAPI{backend: backend}}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, &block, nil, nil)

		var revert *revertError
		require.ErrorAs(t, err, &revert)
		require.Equal(t, hexutil.Encode(revertData("too early")), revert.ErrorData())
	})

	t.Run("unknown hash", func(t *testing.T) {
		backend := pastBlocksBackend(5)
		block := ethrpc.BlockNumberOrHashWithHash(common.Hash{0xaa}, false)

		_, err := (&estimateAPI{backend: backend, blocks: &blockAPI{backend: backend}}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, &block, nil, nil)

		require.ErrorIs(t, err, errBlockNotFound)
	})
}

func TestEstimateGasDefaultsBlockSelectorToLatest(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		baseFee: func() (*big.Int, error) { return new(big.Int), nil },
		estimateGas: func(context.Context, *core.Message, uint64) (uint64, []byte, error) {
			return 21_000, nil, nil
		},
	}
	api := &estimateAPI{backend: backend}

	got, err := api.EstimateGas(t.Context(), export.TransactionArgs{To: &to}, nil, nil, nil)

	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(21_000), got)
}

func TestEstimateGasBaseFeeError(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	want := errors.New("no base fee")
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		baseFee: func() (*big.Int, error) { return nil, want },
		estimateGas: func(context.Context, *core.Message, uint64) (uint64, []byte, error) {
			t.Fatal("estimate reached the backend after a base-fee error")
			return 0, nil, nil
		},
	}

	got, err := (&estimateAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latestBlockSelector(), nil, nil)

	require.Zero(t, got)
	require.ErrorIs(t, err, want)
}

func TestHandlerServesEstimateGas(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		baseFee: func() (*big.Int, error) { return new(big.Int), nil },
		estimateGas: func(context.Context, *core.Message, uint64) (uint64, []byte, error) {
			return 21_000, nil, nil
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

	var got hexutil.Uint64
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_estimateGas", map[string]any{"to": to}, "latest"))
	require.Equal(t, hexutil.Uint64(21_000), got)
}
