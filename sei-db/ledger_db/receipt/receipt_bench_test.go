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
// Writes total receipts across 100 blocks (realistic block distribution).
func BenchmarkReceiptWriteAsync(b *testing.B) {
	const blocks = 100
	// Total receipts spread across 100 blocks
	// 1,000 total = 10 receipts/block
	// 10,000 total = 100 receipts/block
	// 100,000 total = 1,000 receipts/block
	totalReceipts := []int{1_000, 10_000}
	for _, total := range totalReceipts {
		receiptsPerBlock := total / blocks
		b.Run(fmt.Sprintf("blocks=%d/receipts=%d/per_block=%d", blocks, total, receiptsPerBlock), func(b *testing.B) {
			b.Run("pebble-async", func(b *testing.B) {
				benchmarkPebbleWriteAsync(b, receiptsPerBlock, blocks)
			})
			b.Run("parquet-async", func(b *testing.B) {
				if !ParquetEnabled() {
					b.Skip("duckdb disabled; build with -tags duckdb to run parquet benchmarks")
				}
				benchmarkParquetWriteAsync(b, receiptsPerBlock, blocks)
			})
			b.Run("parquet-no-wal", func(b *testing.B) {
				if !ParquetEnabled() {
					b.Skip("duckdb disabled; build with -tags duckdb to run parquet benchmarks")
				}
				benchmarkParquetWriteNoWAL(b, receiptsPerBlock, blocks)
			})
		})
	}
}

func benchmarkPebbleWriteAsync(b *testing.B, receiptsPerBlock int, blocks int) {
	b.Helper()

	_, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = b.TempDir()
	cfg.KeepRecent = 0
	cfg.PruneIntervalSeconds = 0
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
			if err := applyReceiptsAsync(rs, int64(blockNumber), records); err != nil {
				b.Fatalf("failed to write receipts: %v", err)
			}
		}
	}
	b.StopTimer()

	reportBenchMetrics(b, receiptsPerBlock*blocks, blocks)
}

// applyReceiptsAsync writes receipts to pebble with async durability.
func applyReceiptsAsync(store *receiptStore, version int64, receipts []ReceiptRecord) error {
	pairs := make([]*iavl.KVPair, 0, len(receipts))
	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}
		marshalledReceipt, err := record.Receipt.Marshal()
		if err != nil {
			return err
		}
		kvPair := &iavl.KVPair{
			Key:   types.ReceiptKey(record.TxHash),
			Value: marshalledReceipt,
		}
		pairs = append(pairs, kvPair)
	}

	ncs := &proto.NamedChangeSet{
		Name:      types.ReceiptStoreKey,
		Changeset: iavl.ChangeSet{Pairs: pairs},
	}
	return store.db.ApplyChangesetAsync(version, []*proto.NamedChangeSet{ncs})
}

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
			TxHashHex:        txHash.Hex(),
			BlockNumber:      blockNumber,
			TransactionIndex: uint32(i),
			GasUsed:          52000, // Typical ERC20 transfer gas
			CumulativeGasUsed: uint64(52000 * (i + 1)),
			Status:           1, // Success
			Logs: []*types.Log{
				{
					Address: tokenAddress,
					Topics:  []string{transferEventSig, fromAddr, toAddr},
					Data:    amountData,
					Index:   0,
				},
			},
		}
		records[i] = ReceiptRecord{
			TxHash:  txHash,
			Receipt: receipt,
		}
	}
	return records
}

func hashFromUint64(value uint64) common.Hash {
	var buf [32]byte
	binary.BigEndian.PutUint64(buf[24:], value)
	return common.BytesToHash(buf[:])
}

func reportBenchMetrics(b *testing.B, totalReceipts int, blocks int) {
	b.Helper()
	elapsed := b.Elapsed()
	if elapsed > 0 && b.N > 0 {
		perOpSeconds := elapsed.Seconds() / float64(b.N)
		if perOpSeconds > 0 {
			receiptsPerSecond := float64(totalReceipts) / perOpSeconds
			b.ReportMetric(receiptsPerSecond, "receipts/s")
		}
	}
	b.ReportMetric(float64(totalReceipts), "receipts/op")
	b.ReportMetric(float64(blocks), "blocks/op")
}
