package receipt

import (
	"fmt"
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
	fmt.Println("cfg.DBDirectory = ", cfg.DBDirectory)
	cfg.KeepRecent = 1000
	cfg.PruneIntervalSeconds = 10
	cfg.Backend = receiptBackendParquet

	store, err := newReceiptBackend(dbLogger.NewNopLogger(), cfg, storeKey)
	if err != nil {
		b.Fatalf("failed to create receipt store: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })

	pqs, ok := store.(*parquetReceiptStore)
	if !ok {
		b.Fatalf("expected parquet receipt store, got %T", store)
	}

	pqs.store.SetBlockFlushInterval(100)

	totalReceipts := receiptsPerBlock * blocks
	batch := makeDummyReceiptBatch(1, receiptsPerBlock, 0)
	bytePerReceipt := len(batch[0].ReceiptBytes)
	totalBytes := int64(bytePerReceipt * totalReceipts)

	b.SetBytes(totalBytes)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for block := 0; block < blocks; block++ {
			blockNumber := int64(i*blocks + block + 1)
			for j := range batch {
				batch[j].Receipt.BlockNumber = uint64(blockNumber) //nolint:gosec
			}
			if err := pqs.SetReceipts(ctx.WithBlockHeight(blockNumber), batch); err != nil {
				b.Fatalf("failed to write receipts: %v", err)
			}
			if (block+1)%1000 == 0 {
				fmt.Printf("parquet: block %d/%d (%.1f%%)\n", block+1, blocks, float64(block+1)/float64(blocks)*100)
			}
		}
		fmt.Println("parquet: starting final flush...")
		if err := pqs.store.Flush(); err != nil {
			b.Fatalf("failed to flush parquet files: %v", err)
		}
		fmt.Println("parquet: final flush complete")
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

	pqs, ok := store.(*parquetReceiptStore)
	if !ok {
		b.Fatalf("expected parquet receipt store, got %T", store)
	}

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
