//go:build duckdb
// +build duckdb

package receipt

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
)

func benchmarkParquetWriteAsync(b *testing.B, receiptsPerBlock int, blocks int) {
	b.Helper()

	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = b.TempDir()
	cfg.KeepRecent = 0
	cfg.PruneIntervalSeconds = 0
	cfg.Backend = receiptBackendParquet

	store, err := newReceiptBackend(dbLogger.NewNopLogger(), cfg, storeKey)
	if err != nil {
		b.Fatalf("failed to create receipt store: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })

	ps, ok := store.(*parquetReceiptStore)
	if !ok {
		b.Fatalf("expected parquet receipt store, got %T", store)
	}

	// Use batched flush interval (every 25 blocks) instead of per-block
	ps.config.BlockFlushInterval = 25

	var seed uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for block := 0; block < blocks; block++ {
			blockNumber := uint64(i*blocks + block + 1)
			b.StopTimer()
			records := makeDummyReceiptBatch(blockNumber, receiptsPerBlock, seed)
			seed += uint64(receiptsPerBlock)
			b.StartTimer()
			if err := ps.SetReceipts(ctx.WithBlockHeight(int64(blockNumber)), records); err != nil {
				b.Fatalf("failed to write receipts: %v", err)
			}
		}
		// Flush remaining data at end of iteration (ensures all blocks are written)
		if err := flushParquetFiles(ps); err != nil {
			b.Fatalf("failed to flush parquet files: %v", err)
		}
	}
	b.StopTimer()

	reportBenchMetrics(b, receiptsPerBlock*blocks, blocks)
}

// benchmarkParquetWriteNoWAL benchmarks parquet writes bypassing WAL entirely.
// This shows the true parquet write performance without WAL overhead.
func benchmarkParquetWriteNoWAL(b *testing.B, receiptsPerBlock int, blocks int) {
	b.Helper()

	_, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = b.TempDir()
	cfg.KeepRecent = 0
	cfg.PruneIntervalSeconds = 0
	cfg.Backend = receiptBackendParquet

	store, err := newReceiptBackend(dbLogger.NewNopLogger(), cfg, storeKey)
	if err != nil {
		b.Fatalf("failed to create receipt store: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })

	ps, ok := store.(*parquetReceiptStore)
	if !ok {
		b.Fatalf("expected parquet receipt store, got %T", store)
	}

	// Disable intermediate flushes - only flush at end of iteration
	ps.config.BlockFlushInterval = 10000

	var seed uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for block := 0; block < blocks; block++ {
			blockNumber := uint64(i*blocks + block + 1)
			b.StopTimer()
			records := makeDummyReceiptBatch(blockNumber, receiptsPerBlock, seed)
			seed += uint64(receiptsPerBlock)
			b.StartTimer()

			// Bypass WAL - directly write to parquet buffers
			if err := writeParquetNoWAL(ps, blockNumber, records); err != nil {
				b.Fatalf("failed to write receipts: %v", err)
			}
		}
		// Flush remaining data at end of iteration
		if err := flushParquetFiles(ps); err != nil {
			b.Fatalf("failed to flush parquet files: %v", err)
		}
	}
	b.StopTimer()

	reportBenchMetrics(b, receiptsPerBlock*blocks, blocks)
}

// writeParquetNoWAL writes receipts directly to parquet buffers, bypassing WAL.
func writeParquetNoWAL(ps *parquetReceiptStore, blockNumber uint64, records []ReceiptRecord) error {
	blockHash := common.Hash{}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	for _, record := range records {
		if record.Receipt == nil {
			continue
		}

		receipt := record.Receipt
		receiptBytes, err := receipt.Marshal()
		if err != nil {
			return err
		}

		txLogs := getLogsForTx(receipt, 0)
		for _, lg := range txLogs {
			lg.BlockHash = blockHash
		}

		input := parquetReceiptInput{
			blockNumber: blockNumber,
			receipt: receiptRecord{
				TxHash:       copyBytes(record.TxHash[:]),
				BlockNumber:  blockNumber,
				ReceiptBytes: receiptBytes,
			},
			logs: buildLogRecords(txLogs, blockHash),
		}

		if err := ps.applyReceiptLocked(input); err != nil {
			return err
		}
	}

	return nil
}

// flushParquetFiles flushes buffered data to parquet files (no fsync).
func flushParquetFiles(ps *parquetReceiptStore) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.flushLocked()
}

// syncParquetFiles ensures parquet data is durably written to disk.
func syncParquetFiles(ps *parquetReceiptStore) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Flush any buffered data first
	if err := ps.flushLocked(); err != nil {
		return err
	}

	// Sync receipt file
	if ps.receiptFile != nil {
		if err := ps.receiptFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync receipt file: %w", err)
		}
	}

	// Sync log file
	if ps.logFile != nil {
		if err := ps.logFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync log file: %w", err)
		}
	}

	return nil
}
