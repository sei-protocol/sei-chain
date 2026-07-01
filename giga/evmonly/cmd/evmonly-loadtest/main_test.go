package main

import (
	"context"
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

func TestTransferWorkloadExecutesAgainstEVMOnlyExecutor(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--txs-per-block=4",
	})
	require.NoError(t, err)

	state := newGeneratedState()
	workload := newTransferWorkload(cfg, state)
	request, err := workload.buildBlock(context.Background(), 1)
	require.NoError(t, err)

	executor := evmonly.NewExecutor(evmonly.Config{
		MinGasPrice: cfg.minGasPrice,
	}, evmonly.WithState(state))
	result, err := executor.ExecuteBlock(context.Background(), request)
	require.NoError(t, err)

	require.Len(t, result.Txs, cfg.txsPerBlock)
	require.Len(t, result.Receipts, cfg.txsPerBlock)
	require.Equal(t, uint64(cfg.txsPerBlock)*cfg.txGasLimit, result.GasUsed)
	for _, tx := range result.Txs {
		require.Equal(t, ethtypes.ReceiptStatusSuccessful, tx.Status)
		require.NoError(t, tx.Err)
	}

	writer := &discardStateWriter{}
	writer.ApplyChangeSet(result.ChangeSet)
	discardReceiptSink{}.StoreReceipts(request.Context.Number, result.Receipts)
}

func TestERC20TransferWorkloadExecutesAgainstEVMOnlyExecutor(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--workload=erc20-transfer",
		"--txs-per-block=4",
		"--gas-price-wei=0",
		"--min-gas-price-wei=0",
	})
	require.NoError(t, err)
	require.Equal(t, uint64(defaultERC20TxGasLimit), cfg.txGasLimit)

	state := newGeneratedState()
	workload, err := newWorkload(cfg, state)
	require.NoError(t, err)
	request, err := workload.buildBlock(context.Background(), 1)
	require.NoError(t, err)

	executor := evmonly.NewExecutor(evmonly.Config{
		MinGasPrice: cfg.minGasPrice,
		OCCWorkers:  cfg.executorWorkers,
	}, evmonly.WithState(state))
	result, err := executor.ExecuteBlock(context.Background(), request)
	require.NoError(t, err)

	require.Len(t, result.Txs, cfg.txsPerBlock)
	require.Len(t, result.Receipts, cfg.txsPerBlock)
	require.NotEmpty(t, result.ChangeSet.Storage)
	require.True(t, result.OCCStats.Attempted)
	require.False(t, result.OCCStats.Fallback)
	for _, tx := range result.Txs {
		require.Equal(t, ethtypes.ReceiptStatusSuccessful, tx.Status)
		require.NoError(t, tx.Err)
		require.Greater(t, tx.GasUsed, uint64(21_000))
		require.Len(t, tx.Logs, 1)
	}
	for _, receipt := range result.Receipts {
		require.Len(t, receipt.Logs, 1)
	}
}

func TestPrebuildBlocksRequiresBoundedRun(t *testing.T) {
	_, err := parseConfig([]string{
		"--prebuild-blocks",
	})
	require.ErrorContains(t, err, "prebuild-blocks requires --blocks > 0")
}

func TestRunPrebuiltBlocks(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--report-interval=0",
		"--prebuild-blocks",
		"--blocks=2",
		"--txs-per-block=2",
	})
	require.NoError(t, err)
	require.NoError(t, run(cfg))
}

func BenchmarkExecuteTransferBlock(b *testing.B) {
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--txs-per-block=1000",
	})
	require.NoError(b, err)

	state := newGeneratedState()
	workload := newTransferWorkload(cfg, state)
	request, err := workload.buildBlock(context.Background(), 1)
	require.NoError(b, err)
	executor := evmonly.NewExecutor(evmonly.Config{
		MinGasPrice: cfg.minGasPrice,
	}, evmonly.WithState(state))

	b.ReportAllocs()
	b.SetBytes(int64(cfg.txsPerBlock))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := executor.ExecuteBlock(context.Background(), request)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Txs) != cfg.txsPerBlock {
			b.Fatalf("expected %d txs, got %d", cfg.txsPerBlock, len(result.Txs))
		}
	}
}
