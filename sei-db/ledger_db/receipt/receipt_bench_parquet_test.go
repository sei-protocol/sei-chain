package receipt

import (
	"fmt"
	"testing"

	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
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
	addrs := addressPool(5)
	t0s := topicPool(3, 0x01)
	t1s := topicPool(3, 0x02)
	batch := makeDiverseReceiptBatch(1, receiptsPerBlock, 0, addrs, t0s, t1s, nil)
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
