package receipt

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
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

	ps, ok := store.(*cachedReceiptStore)
	if !ok {
		b.Fatalf("expected cached receipt store, got %T", store)
	}
	pqs, ok := ps.backend.(*parquetReceiptStore)
	if !ok {
		b.Fatalf("expected parquet receipt store backend, got %T", ps.backend)
	}

	// Use batched flush interval (every 25 blocks) instead of per-block
	pqs.store.SetBlockFlushInterval(25)

	totalReceipts := receiptsPerBlock * blocks
	allBatches := make([][]ReceiptRecord, blocks)
	var seed uint64
	for block := 0; block < blocks; block++ {
		allBatches[block] = makeDummyReceiptBatch(uint64(block+1), receiptsPerBlock, seed)
		seed += uint64(receiptsPerBlock)
	}
	bytePerReceipt := len(allBatches[0][0].ReceiptBytes)
	totalBytes := int64(bytePerReceipt * totalReceipts)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for block := 0; block < blocks; block++ {
			blockNumber := int64(i*blocks + block + 1)
			if err := pqs.SetReceipts(ctx.WithBlockHeight(blockNumber), allBatches[block]); err != nil {
				b.Fatalf("failed to write receipts: %v", err)
			}
		}
		if err := pqs.store.Flush(); err != nil {
			b.Fatalf("failed to flush parquet files: %v", err)
		}
	}
	b.StopTimer()

	reportBenchMetrics(b, totalReceipts, totalBytes, blocks)
}

// benchmarkParquetWriteNoWAL benchmarks parquet writes bypassing WAL entirely.
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

	ps, ok := store.(*cachedReceiptStore)
	if !ok {
		b.Fatalf("expected cached receipt store, got %T", store)
	}
	pqs, ok := ps.backend.(*parquetReceiptStore)
	if !ok {
		b.Fatalf("expected parquet receipt store backend, got %T", ps.backend)
	}

	// Disable intermediate flushes - only flush at end of iteration
	pqs.store.SetBlockFlushInterval(10000)

	totalReceipts := receiptsPerBlock * blocks

	allBatches := make([][]ReceiptRecord, blocks)
	var seed uint64
	for block := 0; block < blocks; block++ {
		allBatches[block] = makeDummyReceiptBatch(uint64(block+1), receiptsPerBlock, seed)
		seed += uint64(receiptsPerBlock)
	}
	bytePerReceipt := len(allBatches[0][0].ReceiptBytes)
	totalBytes := int64(bytePerReceipt * totalReceipts)

	b.SetBytes(totalBytes)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for block := 0; block < blocks; block++ {
			blockNumber := uint64(i*blocks + block + 1)
			if err := writeParquetNoWAL(pqs, blockNumber, allBatches[block]); err != nil {
				b.Fatalf("failed to write receipts: %v", err)
			}
		}
		if err := pqs.store.Flush(); err != nil {
			b.Fatalf("failed to flush parquet files: %v", err)
		}
	}
	b.StopTimer()

	reportBenchMetrics(b, totalReceipts, totalBytes, blocks)
}

// writeParquetNoWAL writes receipts directly to parquet buffers, bypassing WAL.
func writeParquetNoWAL(ps *parquetReceiptStore, blockNumber uint64, records []ReceiptRecord) error {
	blockHash := common.Hash{}

	inputs := make([]parquet.ReceiptInput, 0, len(records))
	for _, record := range records {
		if record.Receipt == nil {
			continue
		}

		txLogs := getLogsForTx(record.Receipt, 0)
		for _, lg := range txLogs {
			lg.BlockHash = blockHash
		}

		inputs = append(inputs, parquet.ReceiptInput{
			BlockNumber: blockNumber,
			Receipt: parquet.ReceiptRecord{
				TxHash:       parquet.CopyBytes(record.TxHash[:]),
				BlockNumber:  blockNumber,
				ReceiptBytes: parquet.CopyBytesOrEmpty(record.ReceiptBytes),
			},
			Logs:         buildParquetLogRecords(txLogs, blockHash),
			ReceiptBytes: parquet.CopyBytesOrEmpty(record.ReceiptBytes),
		})
	}

	return ps.store.WriteReceiptsDirect(inputs)
}
