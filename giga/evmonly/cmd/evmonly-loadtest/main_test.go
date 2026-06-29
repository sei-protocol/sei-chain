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
