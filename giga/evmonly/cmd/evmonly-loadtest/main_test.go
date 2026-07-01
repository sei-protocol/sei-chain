package main

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
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

	applyGeneratedStateChangeSet(state, result.ChangeSet)
	transferWorkload := workload.(*erc20TransferWorkload)
	for i := uint64(1); i <= uint64(cfg.txsPerBlock); i++ {
		key, err := deterministicPrivateKey(i)
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		recipient := transferWorkload.recipient(i)
		require.Equal(t, common.Hash{}, state.GetState(cfg.erc20Contract, erc20BalanceSlot(sender)))
		require.Equal(t, common.BigToHash(cfg.transferValue), state.GetState(cfg.erc20Contract, erc20BalanceSlot(recipient)))
	}
}

func applyGeneratedStateChangeSet(state *generatedState, changeSet evmonly.StateChangeSet) {
	for _, change := range changeSet.Balances {
		state.SetBalance(change.Address, change.Balance)
	}
	for _, change := range changeSet.Nonces {
		state.SetNonce(change.Address, change.Nonce)
	}
	for _, change := range changeSet.Code {
		if change.Delete {
			state.SetCode(change.Address, nil)
		} else {
			state.SetCode(change.Address, change.Code)
		}
	}
	for _, change := range changeSet.Storage {
		value := change.Value
		if change.Delete {
			value = common.Hash{}
		}
		state.SetState(change.Address, change.Key, value)
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
