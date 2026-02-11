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

	var seed uint64
	totalReceipts := receiptsPerBlock * blocks
	bytesPerReceipt := receiptBytesPerReceipt(b)
	totalBytes := int64(bytesPerReceipt * totalReceipts)
	b.SetBytes(totalBytes)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for block := 0; block < blocks; block++ {
			blockNumber := uint64(i*blocks + block + 1)
			b.StopTimer()
			records := makeDummyReceiptBatch(blockNumber, receiptsPerBlock, seed)
			seed += uint64(receiptsPerBlock)
			b.StartTimer()
			if err := pqs.SetReceipts(ctx.WithBlockHeight(int64(blockNumber)), records); err != nil {
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

	var seed uint64
	totalReceipts := receiptsPerBlock * blocks
	bytesPerReceipt := receiptBytesPerReceipt(b)
	totalBytes := int64(bytesPerReceipt * totalReceipts)
	b.SetBytes(totalBytes)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for block := 0; block < blocks; block++ {
			blockNumber := uint64(i*blocks + block + 1)
			b.StopTimer()
			records := makeDummyReceiptBatch(blockNumber, receiptsPerBlock, seed)
			seed += uint64(receiptsPerBlock)
			b.StartTimer()

			if err := writeParquetNoWAL(pqs, blockNumber, records); err != nil {
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

		receipt := record.Receipt
		receiptBytes := record.ReceiptBytes
		if len(receiptBytes) == 0 {
			var err error
			receiptBytes, err = receipt.Marshal()
			if err != nil {
				return err
			}
		}

		txLogs := getLogsForTx(receipt, 0)
		for _, lg := range txLogs {
			lg.BlockHash = blockHash
		}

		inputs = append(inputs, parquet.ReceiptInput{
			BlockNumber: blockNumber,
			Receipt: parquet.ReceiptRecord{
				TxHash:       parquet.CopyBytes(record.TxHash[:]),
				BlockNumber:  blockNumber,
				ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
			},
			Logs:         buildParquetLogRecords(txLogs, blockHash),
			ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
		})
	}

	return ps.store.WriteReceiptsDirect(inputs)
}
