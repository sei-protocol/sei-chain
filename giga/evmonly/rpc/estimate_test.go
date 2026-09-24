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
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/export"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

// estimatingBackend records the message and gas cap handed to the estimator
// and answers with estimate.
func estimatingBackend(estimate uint64) (*testBackend, *[]*core.Message, *[]uint64) {
	msgs := &[]*core.Message{}
	caps := &[]uint64{}
	backend := &testBackend{
		chainID: func() uint64 { return 713715 },
		baseFee: func() (*big.Int, error) { return big.NewInt(7), nil },
		estimateGas: func(_ context.Context, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
			*msgs = append(*msgs, msg)
			*caps = append(*caps, gasCap)
			return estimate, nil, nil
		},
	}
	return backend, msgs, caps
}

func latest() *ethrpc.BlockNumberOrHash {
	block := ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber)
	return &block
}

func TestEstimateGasReturnsBackendEstimate(t *testing.T) {
	from := common.HexToAddress("0x1000000000000000000000000000000000000000")
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend, msgs, caps := estimatingBackend(137_000)

	got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{From: &from, To: &to}, latest())

	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(137_000), got)
	require.Equal(t, []uint64{defaultCallGasCap}, *caps)
	require.Len(t, *msgs, 1)
	msg := (*msgs)[0]
	require.Equal(t, from, msg.From)
	require.Equal(t, &to, msg.To)
	require.True(t, msg.SkipNonceChecks)
	require.True(t, msg.SkipFromEOACheck)
}

func TestEstimateGasOmittedGasLeavesUpperBoundToEstimator(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend, msgs, _ := estimatingBackend(30_000)

	_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())

	require.NoError(t, err)
	require.Zero(t, (*msgs)[0].GasLimit)
}

func TestEstimateGasCapsExplicitGas(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")

	t.Run("below cap", func(t *testing.T) {
		backend, msgs, _ := estimatingBackend(30_000)
		gas := hexutil.Uint64(50_000)
		_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to, Gas: &gas}, latest())
		require.NoError(t, err)
		require.Equal(t, uint64(50_000), (*msgs)[0].GasLimit)
	})

	t.Run("above cap", func(t *testing.T) {
		backend, msgs, _ := estimatingBackend(30_000)
		gas := hexutil.Uint64(defaultCallGasCap + 1)
		_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to, Gas: &gas}, latest())
		require.NoError(t, err)
		require.Equal(t, uint64(defaultCallGasCap), (*msgs)[0].GasLimit)
	})
}

func TestEstimateGasDefaultsBlockSelectorToLatest(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend, _, _ := estimatingBackend(30_000)

	got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, nil)

	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(30_000), got)
}

func TestEstimateGasAcceptsCurrentStateTags(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	for _, number := range []ethrpc.BlockNumber{ethrpc.LatestBlockNumber, ethrpc.SafeBlockNumber, ethrpc.FinalizedBlockNumber, ethrpc.PendingBlockNumber} {
		backend, _, _ := estimatingBackend(30_000)
		block := ethrpc.BlockNumberOrHashWithNumber(number)
		got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, &block)
		require.NoError(t, err, number)
		require.Equal(t, hexutil.Uint64(30_000), got, number)
	}
}

func TestEstimateGasRejectsHistoricalState(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend := &testBackend{
		estimateGas: func(context.Context, *core.Message, uint64) (uint64, []byte, error) {
			t.Fatal("historical estimate reached the backend")
			return 0, nil, nil
		},
	}
	api := &callAPI{backend: backend}

	for _, block := range []ethrpc.BlockNumberOrHash{
		ethrpc.BlockNumberOrHashWithNumber(ethrpc.EarliestBlockNumber),
		ethrpc.BlockNumberOrHashWithNumber(7),
		ethrpc.BlockNumberOrHashWithHash(common.Hash{2}, true),
	} {
		got, err := api.EstimateGas(t.Context(), export.TransactionArgs{To: &to}, &block)
		require.ErrorIs(t, err, errHistoricalStateUnsupported)
		require.Zero(t, got)
	}
}

func TestEstimateGasSurfacesRevert(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")

	t.Run("with reason", func(t *testing.T) {
		revert := revertData("nope")
		backend, _, _ := estimatingBackend(0)
		backend.estimateGas = func(context.Context, *core.Message, uint64) (uint64, []byte, error) {
			return 0, revert, vm.ErrExecutionReverted
		}

		got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())

		require.Zero(t, got)
		var revertErr *revertError
		require.ErrorAs(t, err, &revertErr)
		require.Equal(t, 3, revertErr.ErrorCode())
		require.Equal(t, hexutil.Encode(revert), revertErr.ErrorData())
		require.Contains(t, err.Error(), "nope")
	})

	t.Run("without data", func(t *testing.T) {
		backend, _, _ := estimatingBackend(0)
		backend.estimateGas = func(context.Context, *core.Message, uint64) (uint64, []byte, error) {
			return 0, nil, vm.ErrExecutionReverted
		}

		got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())

		require.Zero(t, got)
		var revertErr *revertError
		require.ErrorAs(t, err, &revertErr)
		require.Equal(t, 3, revertErr.ErrorCode())
		require.Equal(t, "0x", revertErr.ErrorData())
		require.Equal(t, "execution reverted", err.Error())
	})
}

func TestEstimateGasPassesThroughEstimatorErrors(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	want := errors.New("gas required exceeds allowance (10000000)")
	backend, _, _ := estimatingBackend(0)
	backend.estimateGas = func(context.Context, *core.Message, uint64) (uint64, []byte, error) {
		return 0, nil, want
	}

	got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())

	require.ErrorIs(t, err, want)
	require.Zero(t, got)
}

func TestEstimateGasBackendErrors(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	want := errors.New("boom")

	t.Run("base fee", func(t *testing.T) {
		backend, msgs, _ := estimatingBackend(30_000)
		backend.baseFee = func() (*big.Int, error) { return nil, want }
		_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())
		require.ErrorIs(t, err, want)
		require.Empty(t, *msgs)
	})

	t.Run("call defaults", func(t *testing.T) {
		backend, msgs, _ := estimatingBackend(30_000)
		price := (*hexutil.Big)(big.NewInt(1))
		_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to, GasPrice: price, MaxFeePerGas: price}, latest())
		require.Error(t, err)
		require.Empty(t, *msgs)
	})
}

func TestHandlerServesEstimateGas(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend, _, _ := estimatingBackend(44_000)
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore())
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got hexutil.Uint64
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_estimateGas", map[string]any{"to": to}))
	require.Equal(t, hexutil.Uint64(44_000), got)

	got = 0
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_estimateGas", map[string]any{"to": to}, "latest"))
	require.Equal(t, hexutil.Uint64(44_000), got)

	err = client.CallContext(t.Context(), &got, "eth_estimateGas", map[string]any{"to": to}, "0x7")
	require.ErrorContains(t, err, errHistoricalStateUnsupported.Error())

	backend.estimateGas = func(context.Context, *core.Message, uint64) (uint64, []byte, error) {
		return 0, nil, vm.ErrExecutionReverted
	}
	err = client.CallContext(t.Context(), &got, "eth_estimateGas", map[string]any{"to": to})
	var dataErr ethrpc.DataError
	require.ErrorAs(t, err, &dataErr)
	require.Equal(t, "0x", dataErr.ErrorData())
	var codeErr ethrpc.Error
	require.ErrorAs(t, err, &codeErr)
	require.Equal(t, 3, codeErr.ErrorCode())
}
