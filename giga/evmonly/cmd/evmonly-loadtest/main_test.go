package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/cmd/evmonly-loadtest/scenarios"
)

func TestTransferWorkloadExecutesAgainstEVMOnlyExecutor(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--blocks=1",
		"--txs-per-block=4",
	})
	require.NoError(t, err)

	state := newGeneratedState()
	workload := scenarios.NewTransferWorkload(scenarioConfig(cfg), state)
	request, err := workload.BuildBlock(t.Context(), 1)
	require.NoError(t, err)

	executor := evmonly.NewExecutor(evmonly.Config{
		MinGasPrice: cfg.minGasPrice,
	}, evmonly.WithState(state))
	result, err := executor.ExecuteBlock(t.Context(), request)
	require.NoError(t, err)

	require.Len(t, result.Txs, cfg.txsPerBlock)
	require.Len(t, result.Receipts, cfg.txsPerBlock)
	require.Equal(t, uint64(cfg.txsPerBlock)*cfg.txGasLimit, result.GasUsed)
	for _, tx := range result.Txs {
		require.Equal(t, ethtypes.ReceiptStatusSuccessful, tx.Status)
		require.NoError(t, tx.Err)
	}

	var released atomic.Bool
	require.NoError(t, discardResultSink{writer: &discardStateWriter{}}.StoreBlockResult(t.Context(), request.Context.Number, result, func() {
		released.Store(true)
	}))
	require.True(t, released.Load())
}

func TestTransferWorkloadOCCScenarios(t *testing.T) {
	tests := []struct {
		name              string
		args              []string
		wantConflicts     bool
		wantReruns        bool
		wantSameSender    bool
		wantSameRecipient bool
	}{
		{name: "conflict free"},
		{
			name:              "hot recipient",
			args:              []string{"--recipient=0x00000000000000000000000000000000000000f1"},
			wantConflicts:     true,
			wantReruns:        true,
			wantSameRecipient: true,
		},
		{
			name:           "same sender nonce chain",
			args:           []string{"--same-sender"},
			wantReruns:     true,
			wantSameSender: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{
				"--metrics-addr=",
				"--blocks=1",
				"--txs-per-block=4",
				"--gas-price-wei=0",
				"--min-gas-price-wei=0",
			}, tc.args...)
			cfg, err := parseConfig(args)
			require.NoError(t, err)

			state := newGeneratedState()
			workload := scenarios.NewTransferWorkload(scenarioConfig(cfg), state)
			request, err := workload.BuildBlock(t.Context(), 1)
			require.NoError(t, err)

			signer := ethtypes.LatestSignerForChainID(cfg.chainID)
			senders := make([]common.Address, len(request.Txs))
			recipients := make([]common.Address, len(request.Txs))
			for i, raw := range request.Txs {
				tx := new(ethtypes.Transaction)
				require.NoError(t, tx.UnmarshalBinary(raw))
				senders[i], err = ethtypes.Sender(signer, tx)
				require.NoError(t, err)
				require.NotNil(t, tx.To())
				recipients[i] = *tx.To()
				if tc.wantSameSender {
					require.Equal(t, uint64(i), tx.Nonce())
				} else {
					require.Zero(t, tx.Nonce())
				}
			}
			for i := 1; i < len(request.Txs); i++ {
				require.Equal(t, tc.wantSameSender, senders[i] == senders[0])
				require.Equal(t, tc.wantSameRecipient, recipients[i] == recipients[0])
			}

			executor := evmonly.NewExecutor(evmonly.Config{
				MinGasPrice: cfg.minGasPrice,
				OCCWorkers:  4,
			}, evmonly.WithState(state))
			result, err := executor.ExecuteBlock(t.Context(), request)
			require.NoError(t, err)
			require.True(t, result.OCCStats.Attempted)
			require.False(t, result.OCCStats.Fallback)
			require.Len(t, result.Txs, cfg.txsPerBlock)
			for _, tx := range result.Txs {
				require.Equal(t, ethtypes.ReceiptStatusSuccessful, tx.Status)
				require.NoError(t, tx.Err)
			}
			if tc.wantConflicts {
				require.Greater(t, result.OCCStats.ConflictCount, uint64(0))
			} else {
				require.Zero(t, result.OCCStats.ConflictCount)
			}
			if tc.wantReruns {
				require.Greater(t, result.OCCStats.RerunCount, uint64(0))
			} else {
				require.Zero(t, result.OCCStats.RerunCount)
			}

			applyGeneratedStateChangeSet(state, result.ChangeSet)
			if tc.wantSameSender {
				require.Equal(t, uint64(cfg.txsPerBlock), state.GetNonce(senders[0]))
			}
			result.Release()
			executor.Close()
		})
	}
}

func TestERC20TransferWorkloadExecutesAgainstEVMOnlyExecutor(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--blocks=1",
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
	request, err := workload.BuildBlock(t.Context(), 1)
	require.NoError(t, err)

	executor := evmonly.NewExecutor(evmonly.Config{
		MinGasPrice: cfg.minGasPrice,
		OCCWorkers:  cfg.executorWorkers,
	}, evmonly.WithState(state))
	result, err := executor.ExecuteBlock(t.Context(), request)
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
	transferWorkload := workload.(*scenarios.ERC20TransferWorkload)
	for i := uint64(1); i <= uint64(cfg.txsPerBlock); i++ {
		key, err := scenarios.DeterministicPrivateKey(i)
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		recipient := transferWorkload.Recipient(1, int(i-1), i)
		require.Equal(t, common.Hash{}, state.GetState(cfg.erc20Contract, scenarios.ERC20BalanceSlot(sender)))
		require.Equal(t, common.BigToHash(cfg.transferValue), state.GetState(cfg.erc20Contract, scenarios.ERC20BalanceSlot(recipient)))
	}
}

func TestSnapshotRevertWorkloadExecutesAgainstEVMOnlyExecutor(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--blocks=1",
		"--workload=snapshot-revert",
		"--txs-per-block=4",
		"--gas-price-wei=0",
		"--min-gas-price-wei=0",
	})
	require.NoError(t, err)
	require.Equal(t, uint64(defaultSnapshotRevertTxGasLimit), cfg.txGasLimit)

	state := newGeneratedState()
	workload, err := newWorkload(cfg, state)
	require.NoError(t, err)
	request, err := workload.BuildBlock(t.Context(), 1)
	require.NoError(t, err)

	executor := evmonly.NewExecutor(evmonly.Config{
		MinGasPrice: cfg.minGasPrice,
		OCCWorkers:  4,
	}, evmonly.WithState(state))
	result, err := executor.ExecuteBlock(t.Context(), request)
	require.NoError(t, err)

	require.Len(t, result.Txs, cfg.txsPerBlock)
	require.Len(t, result.Receipts, cfg.txsPerBlock)
	require.True(t, result.OCCStats.Attempted)
	require.False(t, result.OCCStats.Fallback)
	require.Zero(t, result.OCCStats.ConflictCount)
	for _, tx := range result.Txs {
		require.Equal(t, ethtypes.ReceiptStatusSuccessful, tx.Status)
		require.NoError(t, tx.Err)
		require.Greater(t, tx.GasUsed, uint64(21_000))
	}

	require.Len(t, result.ChangeSet.Storage, cfg.txsPerBlock)
	for _, change := range result.ChangeSet.Storage {
		require.Equal(t, cfg.snapshotRevertContract, change.Address)
		require.Equal(t, common.BigToHash(big.NewInt(1)), change.Value)
	}

	applyGeneratedStateChangeSet(state, result.ChangeSet)
	for i := uint64(1); i <= uint64(cfg.txsPerBlock); i++ {
		key, err := scenarios.DeterministicPrivateKey(i)
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		slot := scenarios.SnapshotRevertStorageSlot(sender)
		require.Equal(t, common.BigToHash(big.NewInt(1)), state.GetState(cfg.snapshotRevertContract, slot))
		require.Equal(t, common.Hash{}, state.GetState(cfg.snapshotRevertHelper, slot))
	}
}

func TestTransferWorkloadRecipientConflictRate(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--blocks=1",
		"--txs-per-block=4",
		"--recipient-conflict-rate=0.5",
		"--gas-price-wei=0",
		"--min-gas-price-wei=0",
	})
	require.NoError(t, err)

	state := newGeneratedState()
	workload := scenarios.NewTransferWorkload(scenarioConfig(cfg), state)
	request, err := workload.BuildBlock(t.Context(), 1)
	require.NoError(t, err)

	executor := evmonly.NewExecutor(evmonly.Config{
		MinGasPrice: cfg.minGasPrice,
		OCCWorkers:  4,
	}, evmonly.WithState(state))
	result, err := executor.ExecuteBlock(t.Context(), request)
	require.NoError(t, err)
	require.True(t, result.OCCStats.Attempted)
	require.False(t, result.OCCStats.Fallback)
	require.Greater(t, result.OCCStats.ConflictCount, uint64(0))
	require.Greater(t, result.OCCStats.RerunCount, uint64(0))

	conflictRecipient := workload.Recipient(1, 0, 1)
	require.Equal(t, conflictRecipient, workload.Recipient(1, 1, 2))
	require.NotEqual(t, conflictRecipient, workload.Recipient(1, 2, 3))
	require.NotEqual(t, workload.Recipient(1, 2, 3), workload.Recipient(1, 3, 4))

	applyGeneratedStateChangeSet(state, result.ChangeSet)
	twoTransfers := new(big.Int).Mul(cfg.transferValue, big.NewInt(2))
	require.Equal(t, twoTransfers, state.GetBalance(conflictRecipient))
	require.Equal(t, cfg.transferValue, state.GetBalance(workload.Recipient(1, 2, 3)))
	require.Equal(t, cfg.transferValue, state.GetBalance(workload.Recipient(1, 3, 4)))
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

func TestBlocksRequiresBoundedRun(t *testing.T) {
	_, err := parseConfig([]string{})
	require.ErrorContains(t, err, "blocks must be positive")
}

func TestRecipientConflictRateValidation(t *testing.T) {
	_, err := parseConfig([]string{
		"--blocks=1",
		"--recipient-conflict-rate=1.1",
	})
	require.ErrorContains(t, err, "recipient-conflict-rate must be between 0 and 1")

	_, err = parseConfig([]string{
		"--blocks=1",
		"--recipient=0x0000000000000000000000000000000000000001",
		"--recipient-conflict-rate=0.5",
	})
	require.ErrorContains(t, err, "recipient cannot be combined with recipient-conflict-rate")
}

func TestSameSenderValidation(t *testing.T) {
	_, err := parseConfig([]string{
		"--blocks=1",
		"--workload=erc20-transfer",
		"--same-sender",
	})
	require.ErrorContains(t, err, "same-sender is only supported with transfer workload")

	_, err = parseConfig([]string{
		"--blocks=1",
		"--same-sender",
		"--txs-per-block=4",
		"--gas-price-wei=0",
		"--min-gas-price-wei=0",
		"--sender-balance-wei=3",
		"--transfer-value-wei=1",
	})
	require.ErrorContains(t, err, "sender-balance-wei must cover all same-sender transactions")
}

func TestSnapshotRevertValidation(t *testing.T) {
	_, err := parseConfig([]string{
		"--blocks=1",
		"--workload=snapshot-revert",
		"--recipient=0x0000000000000000000000000000000000000001",
	})
	require.ErrorContains(t, err, "recipient is not supported with snapshot-revert workload")

	_, err = parseConfig([]string{
		"--blocks=1",
		"--workload=snapshot-revert",
		"--recipient-conflict-rate=0.5",
	})
	require.ErrorContains(t, err, "recipient-conflict-rate is not supported with snapshot-revert workload")

	_, err = parseConfig([]string{
		"--blocks=1",
		"--workload=snapshot-revert",
		"--snapshot-revert-contract=0x0000000000000000000000000000000000000001",
		"--snapshot-revert-helper=0x0000000000000000000000000000000000000001",
	})
	require.ErrorContains(t, err, "snapshot-revert-contract and snapshot-revert-helper must differ")
}

func TestParseWorkersConfig(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--blocks=1",
		"--prepare-workers=2",
	})
	require.NoError(t, err)
	require.Equal(t, 1, cfg.parseWorkers)

	cfg, err = parseConfig([]string{
		"--blocks=1",
		"--prepare-workers=1",
	})
	require.NoError(t, err)
	require.Equal(t, runtime.GOMAXPROCS(0), cfg.parseWorkers)

	cfg, err = parseConfig([]string{
		"--blocks=1",
		"--prepare-workers=8",
		"--parse-workers=3",
	})
	require.NoError(t, err)
	require.Equal(t, 3, cfg.parseWorkers)

	_, err = parseConfig([]string{
		"--blocks=1",
		"--parse-workers=-1",
	})
	require.ErrorContains(t, err, "parse-workers must be non-negative")
}

func TestRunPrebuiltBlocks(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--report-interval=0",
		"--blocks=2",
		"--txs-per-block=2",
	})
	require.NoError(t, err)
	require.NoError(t, run(cfg))
}

func TestPrepareBlocksCancelsWorkersOnOrderingInvariantError(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--blocks=1",
		"--report-interval=0",
		"--queue-size=1",
		"--prepare-workers=1",
	})
	require.NoError(t, err)
	blocks := make(chan blockEnvelope, 2)
	blocks <- blockEnvelope{number: 1}
	blocks <- blockEnvelope{number: 1}
	out := make(chan preparedBlockEnvelope, 1)
	metrics := newLoadMetrics(prometheus.NewRegistry())

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- prepareBlocks(ctx, cfg, preparedOnlyExecutor{}, blocks, out, metrics)
	}()

	select {
	case err := <-errCh:
		require.ErrorContains(t, err, "prepared block 1 arrived after block 1")
	case <-ctx.Done():
		t.Fatal("prepareBlocks timed out after ordering invariant error")
	}
}

func TestPrepareBlocksPrefersWorkerErrorOverOrderingDrainError(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--blocks=1",
		"--report-interval=0",
		"--queue-size=2",
		"--prepare-workers=1",
	})
	require.NoError(t, err)
	blocks := make(chan blockEnvelope, 2)
	blocks <- blockEnvelope{number: 2, request: evmonly.BlockRequest{Context: evmonly.BlockContext{Number: 2}}}
	blocks <- blockEnvelope{number: 1, request: evmonly.BlockRequest{Context: evmonly.BlockContext{Number: 1}}}
	close(blocks)
	out := make(chan preparedBlockEnvelope, 2)
	metrics := newLoadMetrics(prometheus.NewRegistry())

	err = prepareBlocks(t.Context(), cfg, failingPreparedExecutor{}, blocks, out, metrics)
	require.ErrorContains(t, err, "prepare worker 0 prepare block 1: injected prepare failure")
	require.ErrorContains(t, err, "prepared block stream closed before block 1")
}

func TestPrepareBlocksDrainsPreparedBlocksAfterWorkersFinish(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--blocks=1",
		"--report-interval=0",
		"--queue-size=2",
		"--prepare-workers=1",
	})
	require.NoError(t, err)
	blocks := make(chan blockEnvelope, 2)
	blocks <- blockEnvelope{number: 1}
	blocks <- blockEnvelope{number: 2}
	close(blocks)
	out := make(chan preparedBlockEnvelope, 2)
	metrics := newLoadMetrics(prometheus.NewRegistry())

	require.NoError(t, prepareBlocks(t.Context(), cfg, preparedOnlyExecutor{}, blocks, out, metrics))
	require.Len(t, out, 2)
}

func TestLoadMetricsRecordsOCCRerunsWithoutConflicts(t *testing.T) {
	metrics := newLoadMetrics(prometheus.NewRegistry())
	metrics.recordFinished(4, 84_000, evmonly.OCCStats{
		Attempted:  true,
		RerunCount: 3,
	})

	snapshot := metrics.snapshot()
	require.Equal(t, uint64(1), snapshot.occAttempts)
	require.Equal(t, uint64(3), snapshot.occReruns)
	require.Zero(t, snapshot.occConflicts)
}

func TestBlockContextTimestampIsDeterministic(t *testing.T) {
	cfg, err := parseConfig([]string{"--metrics-addr=", "--blocks=1"})
	require.NoError(t, err)

	require.Equal(t, defaultGenesisTimestamp+10, scenarios.BlockContext(scenarioConfig(cfg), 10).Time)
	require.Equal(t, scenarios.BlockContext(scenarioConfig(cfg), 10).Time, scenarios.BlockContext(scenarioConfig(cfg), 10).Time)
	require.Less(t, scenarios.BlockContext(scenarioConfig(cfg), 10).Time, scenarios.BlockContext(scenarioConfig(cfg), 11).Time)
}

func TestQueueSizeDefaultPersistQueueOverflowValidation(t *testing.T) {
	_, err := parseConfig([]string{
		"--blocks=1",
		"--queue-size=" + fmt.Sprint(math.MaxInt),
	})
	require.ErrorContains(t, err, "queue-size must be at most")
}

func TestFileResultSinkWritesRLPRecordsAndCleansUpOnCancel(t *testing.T) {
	dir := t.TempDir()
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--blocks=1",
		"--result-sink=file",
		"--persist-dir=" + dir,
		"--persist-sync",
	})
	require.NoError(t, err)
	metrics := newLoadMetrics(prometheus.NewRegistry())
	sinks, err := newResultSinks(cfg, metrics)
	require.NoError(t, err)

	changePath := filepath.Join(dir, "changesets.rlp")
	receiptPath := filepath.Join(dir, "receipts.rlp")
	writtenChangeSet := evmonly.StateChangeSet{
		Balances: []evmonly.BalanceChange{{
			Address: common.HexToAddress("0x0000000000000000000000000000000000000001"),
			Balance: big.NewInt(7),
		}},
	}
	writtenReceipts := ethtypes.Receipts{{
		Type:   ethtypes.LegacyTxType,
		Status: ethtypes.ReceiptStatusSuccessful,
		TxHash: common.HexToHash("0x01"),
	}}
	var released atomic.Bool
	require.NoError(t, sinks.StoreBlockResult(t.Context(), 1, &evmonly.BlockResult{
		ChangeSet: writtenChangeSet,
		Receipts:  writtenReceipts,
	}, func() {
		released.Store(true)
	}))
	require.Eventually(t, func() bool {
		return metrics.snapshot().sinkWritten == 2 && released.Load()
	}, time.Second, time.Millisecond)

	requireFileExists(t, changePath)
	requireFileExists(t, receiptPath)
	var changeSet evmonly.StateChangeSet
	height := readPersistedRLPRecord(t, changePath, &changeSet)
	require.Equal(t, uint64(1), height)
	require.NotEmpty(t, changeSet.Balances)

	var receipts ethtypes.Receipts
	height = readPersistedRLPRecord(t, receiptPath, &receipts)
	require.Equal(t, uint64(1), height)
	require.Len(t, receipts, 1)

	ctx, cancel := context.WithCancel(t.Context())
	stopCleanup := cleanupSinksOnContextCancel(ctx, sinks)
	cancel()
	stopCleanup()
	requireNoFileExists(t, changePath)
	requireNoFileExists(t, receiptPath)
	require.NoError(t, sinks.Close())
	requireNoFileExists(t, changePath)
	requireNoFileExists(t, receiptPath)
}

func TestAsyncFileResultSinkReportsMetrics(t *testing.T) {
	dir := t.TempDir()
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--blocks=1",
		"--result-sink=file",
		"--persist-dir=" + dir,
		"--persist-queue-size=1",
	})
	require.NoError(t, err)
	metrics := newLoadMetrics(prometheus.NewRegistry())
	sinks, err := newResultSinks(cfg, metrics)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, sinks.StoreBlockResult(ctx, 1, &evmonly.BlockResult{}, func() {}))
	require.NoError(t, sinks.Close())

	snapshot := metrics.snapshot()
	require.Equal(t, uint64(2), snapshot.sinkEnqueued)
	require.Equal(t, uint64(2), snapshot.sinkWritten)
	require.Equal(t, int64(0), snapshot.sinkQueued)
	require.Greater(t, snapshot.sinkBytes, uint64(0))
}

func TestAsyncFileResultSinkStoresBlockResultAndReleases(t *testing.T) {
	dir := t.TempDir()
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--blocks=1",
		"--result-sink=file",
		"--persist-dir=" + dir,
		"--persist-queue-size=1",
	})
	require.NoError(t, err)
	metrics := newLoadMetrics(prometheus.NewRegistry())
	sinks, err := newResultSinks(cfg, metrics)
	require.NoError(t, err)

	result := &evmonly.BlockResult{
		ChangeSet: evmonly.StateChangeSet{
			Balances: []evmonly.BalanceChange{{
				Address: common.HexToAddress("0x0000000000000000000000000000000000000001"),
				Balance: big.NewInt(7),
			}},
		},
		Receipts: ethtypes.Receipts{{
			Type:   ethtypes.LegacyTxType,
			Status: ethtypes.ReceiptStatusSuccessful,
			TxHash: common.HexToHash("0x02"),
		}},
	}
	var released atomic.Bool
	require.NoError(t, sinks.StoreBlockResult(t.Context(), 1, result, func() {
		released.Store(true)
	}))
	require.Eventually(t, func() bool {
		return metrics.snapshot().sinkWritten == 2 && released.Load()
	}, time.Second, time.Millisecond)
	require.NoError(t, sinks.Close())

	snapshot := metrics.snapshot()
	require.Equal(t, uint64(2), snapshot.sinkEnqueued)
	require.Equal(t, uint64(2), snapshot.sinkWritten)
}

func TestAsyncFileResultSinkReleasesQueuedResultsAfterWriteError(t *testing.T) {
	dir := t.TempDir()
	recordFile, err := newAppendRLPFile(filepath.Join(dir, "records.rlp"), 1024, true)
	require.NoError(t, err)
	require.NoError(t, recordFile.file.Close())
	metrics := newLoadMetrics(prometheus.NewRegistry())
	sink := &asyncFileResultSinks{
		files: &fileResultSinks{
			changeSetFile: recordFile,
			receiptFile:   recordFile,
			metrics:       metrics,
		},
		metrics: metrics,
		records: make(chan resultSinkRecord, 3),
		done:    make(chan struct{}),
	}

	var releases atomic.Uint64
	for i := 0; i < cap(sink.records); i++ {
		sink.records <- resultSinkRecord{
			result: &evmonly.BlockResult{},
			release: func() {
				releases.Add(1)
			},
		}
	}
	close(sink.records)
	sink.run()

	require.Error(t, sink.getErr())
	require.Equal(t, uint64(3), releases.Load())
}

func TestExecutorResultPoolReusesSlotsWithFileSink(t *testing.T) {
	dir := t.TempDir()
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--blocks=1",
		"--result-sink=file",
		"--persist-dir=" + dir,
		"--result-pool-size=1",
		"--txs-per-block=2",
		"--gas-price-wei=0",
		"--min-gas-price-wei=0",
	})
	require.NoError(t, err)
	state := newGeneratedState()
	workload := scenarios.NewTransferWorkload(scenarioConfig(cfg), state)
	metrics := newLoadMetrics(prometheus.NewRegistry())
	sinks, err := newResultSinks(cfg, metrics)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, sinks.Close())
	}()
	executor := evmonly.NewExecutor(executorConfig(cfg), evmonly.WithState(state), evmonly.WithResultSink(sinks))
	defer executor.Close()

	for blockNumber := uint64(1); blockNumber <= 20; blockNumber++ {
		request, err := workload.BuildBlock(t.Context(), blockNumber)
		require.NoError(t, err)
		result, err := executor.ExecuteBlock(t.Context(), request)
		require.NoError(t, err)
		result.Release()
		require.Eventually(t, func() bool {
			return executor.ResultPoolStats().Available == 1
		}, time.Second, time.Millisecond)
	}
	require.Equal(t, evmonly.BlockResultPoolStats{
		Capacity:  1,
		Available: 1,
	}, executor.ResultPoolStats())
}

func TestRunPrebuiltBlocksWithFileResultSinkCleansUp(t *testing.T) {
	dir := t.TempDir()
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--report-interval=0",
		"--blocks=2",
		"--txs-per-block=2",
		"--gas-price-wei=0",
		"--min-gas-price-wei=0",
		"--result-sink=file",
		"--persist-dir=" + dir,
	})
	require.NoError(t, err)
	require.NoError(t, run(cfg))
	requireNoFileExists(t, filepath.Join(dir, "changesets.rlp"))
	requireNoFileExists(t, filepath.Join(dir, "receipts.rlp"))
}

type preparedOnlyExecutor struct{}

func (preparedOnlyExecutor) ExecuteBlock(context.Context, evmonly.BlockRequest) (*evmonly.BlockResult, error) {
	return nil, errors.New("ExecuteBlock is unused")
}

func (preparedOnlyExecutor) PrepareBlock(_ context.Context, request evmonly.BlockRequest) (evmonly.PreparedBlock, error) {
	return evmonly.PreparedBlock{Context: request.Context}, nil
}

func (preparedOnlyExecutor) ExecutePreparedBlock(context.Context, evmonly.PreparedBlock) (*evmonly.BlockResult, error) {
	return nil, errors.New("ExecutePreparedBlock is unused")
}

type failingPreparedExecutor struct{}

func (failingPreparedExecutor) ExecuteBlock(context.Context, evmonly.BlockRequest) (*evmonly.BlockResult, error) {
	return nil, errors.New("ExecuteBlock is unused")
}

func (failingPreparedExecutor) PrepareBlock(_ context.Context, request evmonly.BlockRequest) (evmonly.PreparedBlock, error) {
	if request.Context.Number == 1 {
		return evmonly.PreparedBlock{}, errors.New("injected prepare failure")
	}
	return evmonly.PreparedBlock{Context: request.Context}, nil
}

func (failingPreparedExecutor) ExecutePreparedBlock(context.Context, evmonly.PreparedBlock) (*evmonly.BlockResult, error) {
	return nil, errors.New("ExecutePreparedBlock is unused")
}

func readPersistedRLPRecord(t *testing.T, path string, out any) uint64 {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(data), 16)
	height := binary.BigEndian.Uint64(data[:8])
	payloadLen := binary.BigEndian.Uint64(data[8:16])
	require.LessOrEqual(t, payloadLen, uint64(len(data)-16))
	payload := data[16 : 16+payloadLen]
	require.NoError(t, rlp.DecodeBytes(payload, out), fmt.Sprintf("decode %s", path))
	return height
}

func requireFileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	require.NoError(t, err)
}

func requireNoFileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	require.Truef(t, errors.Is(err, os.ErrNotExist), "expected %s to be removed, got %v", path, err)
}

func BenchmarkExecuteTransferBlock(b *testing.B) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "conflict_free"},
		{
			name: "hot_recipient",
			args: []string{"--recipient=0x00000000000000000000000000000000000000f1"},
		},
		{
			name: "same_sender_nonce_chain",
			args: []string{"--same-sender"},
		},
	}
	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			args := append([]string{
				"--metrics-addr=",
				"--blocks=1",
				"--txs-per-block=1000",
				"--gas-price-wei=0",
				"--min-gas-price-wei=0",
			}, tc.args...)
			cfg, err := parseConfig(args)
			require.NoError(b, err)

			state := newGeneratedState()
			workload := scenarios.NewTransferWorkload(scenarioConfig(cfg), state)
			request, err := workload.BuildBlock(b.Context(), 1)
			require.NoError(b, err)
			executor := evmonly.NewExecutor(evmonly.Config{
				MinGasPrice: cfg.minGasPrice,
				OCCWorkers:  cfg.executorWorkers,
			}, evmonly.WithState(state))

			b.ReportAllocs()
			b.SetBytes(int64(cfg.txsPerBlock))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := executor.ExecuteBlock(b.Context(), request)
				if err != nil {
					b.Fatal(err)
				}
				if len(result.Txs) != cfg.txsPerBlock {
					b.Fatalf("expected %d txs, got %d", cfg.txsPerBlock, len(result.Txs))
				}
				result.Release()
			}
			executor.Close()
		})
	}
}
