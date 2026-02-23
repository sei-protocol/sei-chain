package receipt

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// BenchmarkReceiptWriteAsync compares async write throughput between pebble and parquet.
func BenchmarkReceiptWriteAsync(b *testing.B) {
	const (
		blocks           = 100000 // 100k
		receiptsPerBlock = 3000
	)
	b.Run(fmt.Sprintf("blocks=%d/per_block=%d", blocks, receiptsPerBlock), func(b *testing.B) {
		b.Run("pebble-async-with-wal", func(b *testing.B) {
			benchmarkPebbleWriteAsync(b, receiptsPerBlock, blocks)
		})
		// b.Run("parquet-async-with-wal", func(b *testing.B) {
		// 	benchmarkParquetWriteAsync(b, receiptsPerBlock, blocks)
		// })
		// b.Run("parquet-no-wal", func(b *testing.B) {
		// 	benchmarkParquetWriteNoWAL(b, receiptsPerBlock, blocks)
		// })
	})
}

func benchmarkPebbleWriteAsync(b *testing.B, receiptsPerBlock int, blocks int) {
	b.Helper()

	_, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = b.TempDir()
	cfg.KeepRecent = 1000
	cfg.PruneIntervalSeconds = 10
	cfg.Backend = receiptBackendPebble

	store, err := newReceiptBackend(dbLogger.NewNopLogger(), cfg, storeKey)
	if err != nil {
		b.Fatalf("failed to create receipt store: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })

	rs, ok := store.(*receiptStore)
	if !ok {
		b.Fatalf("expected pebble receipt store, got %T", store)
	}

	totalReceipts := receiptsPerBlock * blocks
	batch := makeDummyReceiptBatch(1, receiptsPerBlock, 0)
	totalBytes := int64(len(batch[0].ReceiptBytes) * totalReceipts)

	// Get Pebble metrics before writing mostly to track compaction and flush counts.
	before := rs.db.PebbleMetrics()
	beforeCompactCount := int64(before.Compact.Count)
	beforeCompactDuration := before.Compact.Duration.Seconds()
	beforeFlushCount := int64(before.Flush.Count)
	beforeFlushBytes := int64(before.Flush.WriteThroughput.Bytes)

	b.SetBytes(totalBytes)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for block := 0; block < blocks; block++ {
			blockNumber := int64(i*blocks + block + 1)
			if err := applyReceiptsAsync(rs, blockNumber, batch); err != nil {
				b.Fatalf("failed to write receipts: %v", err)
			}
		}
	}
	rs.db.WaitForPendingWrites()
	b.StopTimer()

	// Get Pebble metrics after writing to track compaction and flush counts.
	after := rs.db.PebbleMetrics()
	afterCompactCount := int64(after.Compact.Count)
	afterCompactDuration := after.Compact.Duration.Seconds()
	afterFlushCount := int64(after.Flush.Count)
	afterFlushBytes := int64(after.Flush.WriteThroughput.Bytes)
	b.ReportMetric(float64(afterCompactCount-beforeCompactCount), "compactions")
	b.ReportMetric(afterCompactDuration-beforeCompactDuration, "compaction_s")
	b.ReportMetric(float64(afterFlushCount-beforeFlushCount), "flushes")
	b.ReportMetric(float64(afterFlushBytes-beforeFlushBytes), "flush_bytes")

	reportBenchMetrics(b, totalReceipts, totalBytes, blocks)
}

// applyReceiptsAsync writes receipts to pebble with async durability.
func applyReceiptsAsync(store *receiptStore, version int64, receipts []ReceiptRecord) error {
	pairs := make([]*iavl.KVPair, 0, len(receipts))
	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}
		kvPair := &iavl.KVPair{
			Key:   types.ReceiptKey(record.TxHash),
			Value: record.ReceiptBytes,
		}
		pairs = append(pairs, kvPair)
	}

	ncs := &proto.NamedChangeSet{
		Name:      types.ReceiptStoreKey,
		Changeset: iavl.ChangeSet{Pairs: pairs},
	}
	return store.db.ApplyChangesetAsync(version, []*proto.NamedChangeSet{ncs})
}

var printed = false

func makeDummyReceiptBatch(blockNumber uint64, count int, seed uint64) []ReceiptRecord {
	records := make([]ReceiptRecord, count)

	// ERC20 Transfer event signature: Transfer(address,address,uint256)
	// keccak256("Transfer(address,address,uint256)")
	transferEventSig := "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

	// Token contract address (e.g., USDC-like)
	tokenAddress := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48").Hex()

	// From/To addresses (indexed topics, padded to 32 bytes)
	fromAddr := common.BytesToHash(common.HexToAddress("0x1234567890123456789012345678901234567890").Bytes()).Hex()
	toAddr := common.BytesToHash(common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd").Bytes()).Hex()

	// Transfer amount: 1000 USDC (6 decimals) = 1000000000 as uint256 (32 bytes)
	amountData := make([]byte, 32)
	binary.BigEndian.PutUint64(amountData[24:], 1000000000)

	for i := 0; i < count; i++ {
		txHash := hashFromUint64(seed + uint64(i))
		receipt := &types.Receipt{
			TxHashHex:         txHash.Hex(),
			BlockNumber:       blockNumber,
			TransactionIndex:  uint32(i),
			GasUsed:           52000, // Typical ERC20 transfer gas
			CumulativeGasUsed: uint64(52000 * (i + 1)),
			Status:            1, // Success
			Logs: []*types.Log{
				{
					Address: tokenAddress,
					Topics:  []string{transferEventSig, fromAddr, toAddr},
					Data:    amountData,
					Index:   0,
				},
			},
		}
		receiptBytes, err := receipt.Marshal()
		if err != nil {
			panic(fmt.Sprintf("failed to marshal receipt in benchmark setup: %v", err))
		}
		if !printed {
			printed = true
			fmt.Println("individual receipt bytes = ", len(receiptBytes))
			fmt.Println("receipt contents = ", receipt.String())
		}
		records[i] = ReceiptRecord{
			TxHash:       txHash,
			Receipt:      receipt,
			ReceiptBytes: receiptBytes,
		}
	}
	return records
}

func hashFromUint64(value uint64) common.Hash {
	var buf [32]byte
	binary.BigEndian.PutUint64(buf[24:], value)
	return common.BytesToHash(buf[:])
}

func reportBenchMetrics(b *testing.B, totalReceipts int, totalBytes int64, blocks int) {
	b.Helper()
	elapsed := b.Elapsed()
	fmt.Println("elapsed.Seconds() = ", elapsed.Seconds())
	fmt.Println("b.N = ", b.N)
	if elapsed > 0 && b.N > 0 {
		perOpSeconds := elapsed.Seconds() / float64(b.N)
		if perOpSeconds > 0 {
			fmt.Println("totalReceipts = ", totalReceipts)
			fmt.Println("perOpSeconds = ", perOpSeconds)
			receiptsPerSecond := float64(totalReceipts) / perOpSeconds
			fmt.Println("receiptsPerSecond = ", receiptsPerSecond)
			b.ReportMetric(receiptsPerSecond, "receipts/s")
			bytesPerSecond := float64(totalBytes) / perOpSeconds
			b.ReportMetric(bytesPerSecond, "bytes/s")
		}
	}
	b.ReportMetric(float64(totalReceipts), "receipts/op")
	b.ReportMetric(float64(totalBytes), "bytes/op")
	b.ReportMetric(float64(blocks), "blocks/op")
}
