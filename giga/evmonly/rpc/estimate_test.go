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
	"github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

const testBlockGasLimit = 30_000_000

// gasNeedingBackend simulates a call to a contract that succeeds only when
// given at least need gas, using need-1_000 of it, and reports every gas
// limit tried.
func gasNeedingBackend(t *testing.T, need uint64) (*testBackend, *[]uint64) {
	t.Helper()
	tried := &[]uint64{}
	backend := &testBackend{
		chainID:  func() uint64 { return 713715 },
		baseFee:  func() (*big.Int, error) { return new(big.Int), nil },
		gasLimit: func() (uint64, error) { return testBlockGasLimit, nil },
		balance:  func(common.Address) uint256.Int { return *uint256.NewInt(0) },
		code:     func(common.Address) ([]byte, error) { return []byte{0xfe}, nil },
		call: func(_ context.Context, msg *core.Message) (*core.ExecutionResult, error) {
			*tried = append(*tried, msg.GasLimit)
			if msg.GasLimit < need {
				return &core.ExecutionResult{UsedGas: msg.GasLimit, Err: vm.ErrOutOfGas}, nil
			}
			return &core.ExecutionResult{UsedGas: need - 1_000, RefundedGas: 500}, nil
		},
	}
	return backend, tried
}

func latest() *ethrpc.BlockNumberOrHash {
	block := ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber)
	return &block
}

func TestEstimateGasConvergesOnMinimumGas(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	const need = 137_000
	backend, tried := gasNeedingBackend(t, need)
	api := &callAPI{backend: backend}

	got, err := api.EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())

	require.NoError(t, err)
	require.GreaterOrEqual(t, uint64(got), uint64(need))
	require.LessOrEqual(t, float64(got-need)/float64(got), estimateGasErrorRatio)
	// The first probe is the upper bound so an impossible call fails fast; the
	// gas it used then floors the search, so a handful of probes suffice.
	require.Equal(t, uint64(defaultCallGasCap), (*tried)[0])
	require.LessOrEqual(t, len(*tried), 4, *tried)
	for _, gas := range (*tried)[1:] {
		require.Greater(t, gas, uint64(need-1_000), *tried)
	}
}

func TestEstimateGasPlainTransferCostsTxGas(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend, tried := gasNeedingBackend(t, params.TxGas)
	backend.code = func(addr common.Address) ([]byte, error) {
		require.Equal(t, to, addr)
		return nil, nil
	}

	got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())

	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(params.TxGas), got)
	require.Equal(t, []uint64{params.TxGas}, *tried)
}

func TestEstimateGasPlainTransferFallsBackToSearch(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	const need = 30_000
	backend, tried := gasNeedingBackend(t, need)
	backend.code = func(common.Address) ([]byte, error) { return nil, nil }

	got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())

	require.NoError(t, err)
	require.GreaterOrEqual(t, uint64(got), uint64(need))
	require.Equal(t, []uint64{params.TxGas, defaultCallGasCap}, (*tried)[:2])
}

func TestEstimateGasSkipsPlainTransferShortcutForCalls(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	data := hexutil.Bytes{0x01}

	t.Run("calldata", func(t *testing.T) {
		backend, tried := gasNeedingBackend(t, 30_000)
		backend.code = func(common.Address) ([]byte, error) {
			t.Fatal("code read for a message with calldata")
			return nil, nil
		}
		_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to, Input: &data}, latest())
		require.NoError(t, err)
		require.Equal(t, uint64(defaultCallGasCap), (*tried)[0])
	})

	t.Run("contract creation", func(t *testing.T) {
		backend, tried := gasNeedingBackend(t, 60_000)
		backend.code = func(common.Address) ([]byte, error) {
			t.Fatal("code read for a contract creation")
			return nil, nil
		}
		_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{}, latest())
		require.NoError(t, err)
		require.Equal(t, uint64(defaultCallGasCap), (*tried)[0])
	})

	t.Run("code read fails", func(t *testing.T) {
		want := errors.New("boom")
		backend, tried := gasNeedingBackend(t, 30_000)
		backend.code = func(common.Address) ([]byte, error) { return nil, want }
		_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())
		require.ErrorIs(t, err, want)
		require.Empty(t, *tried)
	})
}

func TestEstimateGasDefaultsBlockSelectorToLatest(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend, _ := gasNeedingBackend(t, 30_000)

	got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, nil)

	require.NoError(t, err)
	require.GreaterOrEqual(t, uint64(got), uint64(30_000))
}

func TestEstimateGasAcceptsCurrentStateTags(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	for _, number := range []ethrpc.BlockNumber{ethrpc.LatestBlockNumber, ethrpc.SafeBlockNumber, ethrpc.FinalizedBlockNumber, ethrpc.PendingBlockNumber} {
		backend, _ := gasNeedingBackend(t, 30_000)
		block := ethrpc.BlockNumberOrHashWithNumber(number)
		got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, &block)
		require.NoError(t, err, number)
		require.GreaterOrEqual(t, uint64(got), uint64(30_000), number)
	}
}

func TestEstimateGasRejectsHistoricalState(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend := &testBackend{
		call: func(context.Context, *core.Message) (*core.ExecutionResult, error) {
			t.Fatal("historical estimate reached the backend")
			return nil, nil
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

func TestEstimateGasUsesBlockGasLimitAsUpperBound(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend, tried := gasNeedingBackend(t, 30_000)
	backend.gasLimit = func() (uint64, error) { return 5_000_000, nil }

	_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())

	require.NoError(t, err)
	// CallDefaults fills an omitted gas with the cap, so the block limit only
	// applies when it is lower than defaultCallGasCap.
	require.Equal(t, uint64(defaultCallGasCap), (*tried)[0])
}

func TestEstimateGasHonoursExplicitGasAsUpperBound(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend, tried := gasNeedingBackend(t, 50_000)
	explicit := hexutil.Uint64(40_000)

	got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to, Gas: &explicit}, latest())

	require.Zero(t, got)
	require.EqualError(t, err, "gas required exceeds allowance (40000)")
	require.Equal(t, []uint64{40_000}, *tried)
}

func TestEstimateGasCapsUpperBoundByBalance(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	from := common.HexToAddress("0x2000000000000000000000000000000000000002")
	gasPrice := (*hexutil.Big)(big.NewInt(2))

	t.Run("balance covers the call", func(t *testing.T) {
		backend, tried := gasNeedingBackend(t, 30_000)
		backend.balance = func(addr common.Address) uint256.Int {
			require.Equal(t, from, addr)
			return *uint256.NewInt(2 * 100_000)
		}
		got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{From: &from, To: &to, GasPrice: gasPrice}, latest())
		require.NoError(t, err)
		require.GreaterOrEqual(t, uint64(got), uint64(30_000))
		require.Equal(t, uint64(100_000), (*tried)[0])
	})

	t.Run("balance too low for the call", func(t *testing.T) {
		backend, _ := gasNeedingBackend(t, 30_000)
		backend.balance = func(common.Address) uint256.Int { return *uint256.NewInt(2 * 25_000) }
		got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{From: &from, To: &to, GasPrice: gasPrice}, latest())
		require.Zero(t, got)
		require.EqualError(t, err, "gas required exceeds allowance (25000)")
	})

	t.Run("value is deducted before gas", func(t *testing.T) {
		backend, tried := gasNeedingBackend(t, 30_000)
		backend.balance = func(common.Address) uint256.Int { return *uint256.NewInt(2*100_000 + 7) }
		value := (*hexutil.Big)(big.NewInt(7))
		_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{From: &from, To: &to, GasPrice: gasPrice, Value: value}, latest())
		require.NoError(t, err)
		require.Equal(t, uint64(100_000), (*tried)[0])
	})

	t.Run("value exceeds balance", func(t *testing.T) {
		backend, tried := gasNeedingBackend(t, 30_000)
		backend.balance = func(common.Address) uint256.Int { return *uint256.NewInt(5) }
		value := (*hexutil.Big)(big.NewInt(5))
		got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{From: &from, To: &to, GasPrice: gasPrice, Value: value}, latest())
		require.Zero(t, got)
		require.ErrorIs(t, err, core.ErrInsufficientFundsForTransfer)
		require.Empty(t, *tried)
	})

	t.Run("zero fee cap skips the balance check", func(t *testing.T) {
		backend, _ := gasNeedingBackend(t, 30_000)
		backend.balance = func(common.Address) uint256.Int {
			t.Fatal("balance read for a zero-fee estimate")
			return uint256.Int{}
		}
		_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{From: &from, To: &to}, latest())
		require.NoError(t, err)
	})
}

func TestEstimateGasSurfacesRevertAtUpperBound(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	revert := revertData("nope")
	calls := 0
	backend := &testBackend{
		chainID:  func() uint64 { return 713715 },
		baseFee:  func() (*big.Int, error) { return new(big.Int), nil },
		gasLimit: func() (uint64, error) { return testBlockGasLimit, nil },
		code:     func(common.Address) ([]byte, error) { return []byte{0xfe}, nil },
		call: func(context.Context, *core.Message) (*core.ExecutionResult, error) {
			calls++
			return &core.ExecutionResult{UsedGas: 21_000, Err: vm.ErrExecutionReverted, ReturnData: revert}, nil
		},
	}

	got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())

	require.Zero(t, got)
	var revertErr *revertError
	require.ErrorAs(t, err, &revertErr)
	require.Equal(t, 3, revertErr.ErrorCode())
	require.Equal(t, hexutil.Encode(revert), revertErr.ErrorData())
	require.Contains(t, err.Error(), "nope")
	require.Equal(t, 1, calls)
}

func TestEstimateGasSurfacesOutOfGasAtUpperBound(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend, tried := gasNeedingBackend(t, defaultCallGasCap+1)

	got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())

	require.Zero(t, got)
	require.EqualError(t, err, "gas required exceeds allowance (10000000)")
	require.Equal(t, []uint64{defaultCallGasCap}, *tried)
}

func TestEstimateGasPassesThroughNonGasExecutionError(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	want := errors.New("invalid opcode")
	backend := &testBackend{
		chainID:  func() uint64 { return 713715 },
		baseFee:  func() (*big.Int, error) { return new(big.Int), nil },
		gasLimit: func() (uint64, error) { return testBlockGasLimit, nil },
		code:     func(common.Address) ([]byte, error) { return []byte{0xfe}, nil },
		call: func(context.Context, *core.Message) (*core.ExecutionResult, error) {
			return &core.ExecutionResult{Err: want}, nil
		},
	}

	got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())

	require.Zero(t, got)
	require.ErrorIs(t, err, want)
}

func TestEstimateGasTreatsIntrinsicGasErrorAsTooLittleGas(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	const need = 60_000
	backend, _ := gasNeedingBackend(t, need)
	inner := backend.call
	backend.call = func(ctx context.Context, msg *core.Message) (*core.ExecutionResult, error) {
		if msg.GasLimit < params.TxGas*2 {
			return nil, core.ErrIntrinsicGas
		}
		return inner(ctx, msg)
	}

	got, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())

	require.NoError(t, err)
	require.GreaterOrEqual(t, uint64(got), uint64(need))
}

func TestEstimateGasBackendErrors(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	want := errors.New("boom")

	t.Run("base fee", func(t *testing.T) {
		backend, tried := gasNeedingBackend(t, 30_000)
		backend.baseFee = func() (*big.Int, error) { return nil, want }
		_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())
		require.ErrorIs(t, err, want)
		require.Empty(t, *tried)
	})

	t.Run("block gas limit", func(t *testing.T) {
		backend, tried := gasNeedingBackend(t, 30_000)
		backend.gasLimit = func() (uint64, error) { return 0, want }
		small := hexutil.Uint64(params.TxGas - 1)
		_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to, Gas: &small}, latest())
		require.ErrorIs(t, err, want)
		require.Empty(t, *tried)
	})

	t.Run("call", func(t *testing.T) {
		backend, _ := gasNeedingBackend(t, 30_000)
		backend.call = func(context.Context, *core.Message) (*core.ExecutionResult, error) { return nil, want }
		_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())
		require.ErrorIs(t, err, want)
	})

	t.Run("nil result", func(t *testing.T) {
		backend, _ := gasNeedingBackend(t, 30_000)
		backend.call = func(context.Context, *core.Message) (*core.ExecutionResult, error) { return nil, nil }
		_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())
		require.ErrorContains(t, err, "no result")
	})
}

func TestEstimateGasDoesNotMutateMessage(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend, _ := gasNeedingBackend(t, 30_000)
	var seen []*core.Message
	inner := backend.call
	backend.call = func(ctx context.Context, msg *core.Message) (*core.ExecutionResult, error) {
		seen = append(seen, msg)
		return inner(ctx, msg)
	}

	_, err := (&callAPI{backend: backend}).EstimateGas(t.Context(), export.TransactionArgs{To: &to}, latest())

	require.NoError(t, err)
	require.Greater(t, len(seen), 1)
	for i := 1; i < len(seen); i++ {
		require.NotSame(t, seen[0], seen[i])
	}
}

func TestHandlerServesEstimateGas(t *testing.T) {
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend, _ := gasNeedingBackend(t, 44_000)
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
	require.GreaterOrEqual(t, uint64(got), uint64(44_000))

	got = 0
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_estimateGas", map[string]any{"to": to}, "latest"))
	require.GreaterOrEqual(t, uint64(got), uint64(44_000))

	err = client.CallContext(t.Context(), &got, "eth_estimateGas", map[string]any{"to": to}, "0x7")
	require.ErrorContains(t, err, errHistoricalStateUnsupported.Error())
}
