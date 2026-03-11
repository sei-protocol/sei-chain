package receipt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	pqgo "github.com/parquet-go/parquet-go"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

type truncateOnlyWAL struct {
	truncateErr error
}

func (w *truncateOnlyWAL) TruncateBefore(_ uint64) error { return w.truncateErr }

func TestLedgerCacheReceiptsAndLogs(t *testing.T) {
	cache := newLedgerCache()
	txHash := common.HexToHash("0x1")
	blockNumber := uint64(10)

	cache.AddReceiptsBatch(blockNumber, []receiptCacheEntry{
		{
			TxHash:  txHash,
			Receipt: &types.Receipt{TxHashHex: txHash.Hex(), BlockNumber: blockNumber},
		},
	})

	got, ok := cache.GetReceipt(txHash)
	require.True(t, ok)
	require.Equal(t, txHash.Hex(), got.TxHashHex)

	addr := common.HexToAddress("0x100")
	topic := common.HexToHash("0xabc")
	cache.AddLogsForBlock(blockNumber, []*ethtypes.Log{
		{
			Address:     addr,
			Topics:      []common.Hash{topic},
			BlockNumber: blockNumber,
			TxHash:      txHash,
			TxIndex:     0,
			Index:       0,
		},
	})

	require.Len(t, cache.FilterLogs(blockNumber, blockNumber, filters.FilterCriteria{}), 1)
	require.Len(t, cache.FilterLogs(blockNumber+1, blockNumber+1, filters.FilterCriteria{}), 0)
}

func TestLedgerCacheRotatePrunes(t *testing.T) {
	cache := newLedgerCache()
	txHash := common.HexToHash("0x2")
	blockNumber := uint64(1)
	cache.AddReceiptsBatch(blockNumber, []receiptCacheEntry{
		{
			TxHash:  txHash,
			Receipt: &types.Receipt{TxHashHex: txHash.Hex(), BlockNumber: blockNumber},
		},
	})

	_, ok := cache.GetReceipt(txHash)
	require.True(t, ok)

	cache.Rotate()
	cache.Rotate()

	_, ok = cache.GetReceipt(txHash)
	require.False(t, ok)
}

func TestParquetReceiptStoreCacheLogs(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()

	store, err := NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	txHash := common.HexToHash("0x10")
	addr := common.HexToAddress("0x200")
	topic := common.HexToHash("0x1234")
	receipt := makeTestReceipt(txHash, 10, 2, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))

	logs, err := store.FilterLogs(ctx, 10, 10, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, uint64(10), logs[0].BlockNumber)
	require.Equal(t, uint(2), logs[0].TxIndex)
}

func TestParquetReceiptStoreReopenQueries(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()

	store, err := NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)

	txHash := common.HexToHash("0x20")
	addr := common.HexToAddress("0x300")
	topic := common.HexToHash("0x5678")
	receipt := makeTestReceipt(txHash, 5, 1, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))
	require.NoError(t, store.Close())

	store, err = NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	got, err := store.GetReceiptFromStore(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, receipt.TxHashHex, got.TxHashHex)

	// Query blocks 3-5, receipt is in block 5
	logs, err := store.FilterLogs(ctx, 3, 5, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, uint64(5), logs[0].BlockNumber)
}

func TestParquetReceiptStoreWALReplay(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()

	store, err := NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)

	txHash := common.HexToHash("0x30")
	addr := common.HexToAddress("0x400")
	topic := common.HexToHash("0x9abc")
	receipt := makeTestReceipt(txHash, 3, 0, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))
	require.NoError(t, store.Close())

	receiptFiles, err := filepath.Glob(filepath.Join(cfg.DBDirectory, "receipts_*.parquet"))
	require.NoError(t, err)
	for _, file := range receiptFiles {
		require.NoError(t, os.Remove(file))
	}

	logFiles, err := filepath.Glob(filepath.Join(cfg.DBDirectory, "logs_*.parquet"))
	require.NoError(t, err)
	for _, file := range logFiles {
		require.NoError(t, os.Remove(file))
	}

	store, err = NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	got, err := store.GetReceiptFromStore(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, receipt.TxHashHex, got.TxHashHex)
}

func TestParquetFilePruning(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()
	cfg.KeepRecent = 600 // Keep 600 blocks, so files with blocks < (latest - 600) get pruned

	store, err := NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)

	// Write receipts across multiple files (500 blocks per file)
	// File 0: blocks 1-500
	// File 1: blocks 501-1000
	// File 2: blocks 1001-1500
	for block := uint64(1); block <= 1500; block++ {
		txHash := common.BigToHash(common.Big1.SetUint64(block))
		receipt := makeTestReceipt(txHash, block, 0, common.HexToAddress("0x1"), nil)
		err := store.SetReceipts(ctx.WithBlockHeight(int64(block)), []ReceiptRecord{
			{TxHash: txHash, Receipt: receipt},
		})
		require.NoError(t, err)
	}

	// Close to flush all files
	require.NoError(t, store.Close())

	// Verify we have at least 2 receipt and log files
	receiptFiles, err := filepath.Glob(filepath.Join(cfg.DBDirectory, "receipts_*.parquet"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(receiptFiles), 2, "should have at least 2 receipt files")

	logFiles, err := filepath.Glob(filepath.Join(cfg.DBDirectory, "logs_*.parquet"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(logFiles), 2, "should have at least 2 log files")

	// Reopen store - pruning will run in background
	store, err = NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Verify we can still query recent data
	txHash := common.BigToHash(common.Big1.SetUint64(1400))
	_, err = store.GetReceiptFromStore(ctx, txHash)
	require.NoError(t, err)
}

func TestParquetReaderGetFilesBeforeBlock(t *testing.T) {
	dir := t.TempDir()

	// Create mock parquet files
	createMockParquetFile(t, dir, "receipts_0.parquet")
	createMockParquetFile(t, dir, "receipts_500.parquet")
	createMockParquetFile(t, dir, "receipts_1000.parquet")
	createMockParquetFile(t, dir, "logs_0.parquet")
	createMockParquetFile(t, dir, "logs_500.parquet")
	createMockParquetFile(t, dir, "logs_1000.parquet")

	reader, err := parquet.NewReader(dir)
	require.NoError(t, err)
	defer reader.Close()

	// Test: prune before block 600
	// File 0 (blocks 0-499) should be pruned (0 + 500 <= 600)
	// File 500 (blocks 500-999) should NOT be pruned (500 + 500 > 600)
	files := reader.GetFilesBeforeBlock(600)
	require.Len(t, files, 1)
	require.Contains(t, files[0].ReceiptFile, "receipts_0.parquet")

	// Test: prune before block 1100
	// Files 0 and 500 should be pruned
	files = reader.GetFilesBeforeBlock(1100)
	require.Len(t, files, 2)

	// Test: prune before block 400
	// No files should be pruned (0 + 500 > 400)
	files = reader.GetFilesBeforeBlock(400)
	require.Len(t, files, 0)
}

func TestParquetPruneOldFiles(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()
	cfg.KeepRecent = 0 // Disable auto-pruning

	store, err := NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)

	// Write enough data to create multiple files
	for block := uint64(1); block <= 1200; block++ {
		txHash := common.BigToHash(common.Big1.SetUint64(block))
		receipt := makeTestReceipt(txHash, block, 0, common.HexToAddress("0x1"), nil)
		err := store.SetReceipts(ctx.WithBlockHeight(int64(block)), []ReceiptRecord{
			{TxHash: txHash, Receipt: receipt},
		})
		require.NoError(t, err)
	}
	require.NoError(t, store.Close())

	// Count files before pruning
	receiptFilesBefore, _ := filepath.Glob(filepath.Join(cfg.DBDirectory, "receipts_*.parquet"))
	require.GreaterOrEqual(t, len(receiptFilesBefore), 2, "should have at least 2 receipt files")

	// Reopen store
	store, err = NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Verify we can still query data
	txHash := common.BigToHash(common.Big1.SetUint64(1000))
	_, err = store.GetReceiptFromStore(ctx, txHash)
	require.NoError(t, err)
}

func TestExtractBlockNumber(t *testing.T) {
	tests := []struct {
		path     string
		expected uint64
	}{
		{"receipts_0.parquet", 0},
		{"receipts_500.parquet", 500},
		{"receipts_1000.parquet", 1000},
		{"/path/to/receipts_12345.parquet", 12345},
		{"logs_0.parquet", 0},
		{"logs_999.parquet", 999},
		{"invalid.parquet", 0},
		{"receipts_.parquet", 0},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := parquet.ExtractBlockNumber(tt.path)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestTruncateReplayWALReturnsError(t *testing.T) {
	wal := &truncateOnlyWAL{truncateErr: errors.New("truncate failed")}
	err := truncateReplayWAL(wal, 10)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to truncate replay WAL")
}

func TestParquetCorruptFileRecoveryFromWAL(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()

	store, err := NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)

	txHash := common.HexToHash("0x40")
	addr := common.HexToAddress("0x500")
	topic := common.HexToHash("0xdef0")
	receipt := makeTestReceipt(txHash, 7, 0, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))
	require.NoError(t, store.Close())

	// Simulate crash: replace the last receipt parquet file with corrupt data
	// (missing parquet footer / magic bytes). The WAL still has the entry.
	receiptFiles, err := filepath.Glob(filepath.Join(cfg.DBDirectory, "receipts_*.parquet"))
	require.NoError(t, err)
	require.NotEmpty(t, receiptFiles)
	lastFile := receiptFiles[len(receiptFiles)-1]
	require.NoError(t, os.WriteFile(lastFile, []byte("corrupt"), 0o644))

	// Reopen — should delete the corrupt file and recover from WAL.
	// The file with the same name may be re-created by WAL replay (the
	// receipt is at the same block number), which is expected.
	store, err = NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Receipt should be recovered via WAL replay.
	got, err := store.GetReceiptFromStore(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, receipt.TxHashHex, got.TxHashHex)
}

// createMockParquetFile creates a minimal valid parquet file for testing
func createMockParquetFile(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)

	// Create a valid parquet file with minimal data that DuckDB can read
	f, err := os.Create(path)
	require.NoError(t, err)

	// Use parquet-go to write a minimal valid file
	type minimalRecord struct {
		BlockNumber uint64 `parquet:"block_number"`
	}
	writer := pqgo.NewGenericWriter[minimalRecord](f)
	_, err = writer.Write([]minimalRecord{{BlockNumber: 0}})
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, f.Close())
}
