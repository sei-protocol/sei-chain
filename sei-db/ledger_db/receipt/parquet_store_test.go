//go:build duckdb
// +build duckdb

package receipt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func requireParquetEnabled(t *testing.T) {
	t.Helper()
	if !ParquetEnabled() {
		t.Skip("duckdb disabled; build with -tags duckdb to run parquet tests")
	}
}

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

	require.True(t, cache.HasLogsForBlock(blockNumber))
	require.False(t, cache.HasLogsForBlock(blockNumber+1))
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
	requireParquetEnabled(t)
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
	requireParquetEnabled(t)
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
	requireParquetEnabled(t)
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
	requireParquetEnabled(t)
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

	// Verify we have 3 receipt files and 3 log files
	receiptFiles, err := filepath.Glob(filepath.Join(cfg.DBDirectory, "receipts_*.parquet"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(receiptFiles), 2, "should have at least 2 receipt files")

	logFiles, err := filepath.Glob(filepath.Join(cfg.DBDirectory, "logs_*.parquet"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(logFiles), 2, "should have at least 2 log files")

	// Reopen store - this should trigger pruning since keepRecent=600
	// Latest version is 1500, so prune before block 900
	// File 0 (blocks 1-500) should be pruned
	store, err = NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Get the parquet store to manually trigger pruning
	cachedStore := store.(*cachedReceiptStore)
	parquetStore := cachedStore.backend.(*parquetReceiptStore)

	// Manually trigger pruning (normally runs in background)
	pruneBeforeBlock := uint64(parquetStore.latestVersion.Load() - parquetStore.keepRecent)
	pruned := parquetStore.pruneOldFiles(pruneBeforeBlock)

	// Should have pruned at least one file pair
	if pruneBeforeBlock > 500 {
		require.Greater(t, pruned, 0, "should have pruned at least one file pair")
	}

	// Verify the old files are deleted
	receiptFilesAfter, err := filepath.Glob(filepath.Join(cfg.DBDirectory, "receipts_*.parquet"))
	require.NoError(t, err)

	// Check that file_0 (receipts_0.parquet) was deleted if pruned
	if pruned > 0 {
		for _, f := range receiptFilesAfter {
			startBlock := extractBlockNumber(f)
			require.GreaterOrEqual(t, startBlock+500, pruneBeforeBlock,
				"file %s should not exist after pruning (startBlock=%d, pruneBeforeBlock=%d)",
				f, startBlock, pruneBeforeBlock)
		}
	}
}

func TestParquetReaderGetFilesBeforeBlock(t *testing.T) {
	requireParquetEnabled(t)
	dir := t.TempDir()

	// Create mock parquet files
	createMockParquetFile(t, dir, "receipts_0.parquet")
	createMockParquetFile(t, dir, "receipts_500.parquet")
	createMockParquetFile(t, dir, "receipts_1000.parquet")
	createMockParquetFile(t, dir, "logs_0.parquet")
	createMockParquetFile(t, dir, "logs_500.parquet")
	createMockParquetFile(t, dir, "logs_1000.parquet")

	reader, err := newParquetReader(dir)
	require.NoError(t, err)
	defer reader.Close()

	// Test: prune before block 600
	// File 0 (blocks 0-499) should be pruned (0 + 500 <= 600)
	// File 500 (blocks 500-999) should NOT be pruned (500 + 500 > 600)
	files := reader.getFilesBeforeBlock(600)
	require.Len(t, files, 1)
	require.Contains(t, files[0].receiptFile, "receipts_0.parquet")

	// Test: prune before block 1100
	// Files 0 and 500 should be pruned
	files = reader.getFilesBeforeBlock(1100)
	require.Len(t, files, 2)

	// Test: prune before block 400
	// No files should be pruned (0 + 500 > 400)
	files = reader.getFilesBeforeBlock(400)
	require.Len(t, files, 0)
}

func TestParquetReaderRemoveFilesBeforeBlock(t *testing.T) {
	requireParquetEnabled(t)
	dir := t.TempDir()

	// Create mock parquet files
	createMockParquetFile(t, dir, "receipts_0.parquet")
	createMockParquetFile(t, dir, "receipts_500.parquet")
	createMockParquetFile(t, dir, "receipts_1000.parquet")
	createMockParquetFile(t, dir, "logs_0.parquet")
	createMockParquetFile(t, dir, "logs_500.parquet")
	createMockParquetFile(t, dir, "logs_1000.parquet")

	reader, err := newParquetReader(dir)
	require.NoError(t, err)
	defer reader.Close()

	initialCount := reader.closedReceiptFileCount()
	require.Equal(t, 3, initialCount)

	// Remove files before block 600
	reader.removeFilesBeforeBlock(600)

	// Should have 2 files left (500 and 1000)
	require.Equal(t, 2, reader.closedReceiptFileCount())

	// Remove files before block 1100
	reader.removeFilesBeforeBlock(1100)

	// Should have 1 file left (1000)
	require.Equal(t, 1, reader.closedReceiptFileCount())
}

func TestParquetPruneOldFiles(t *testing.T) {
	requireParquetEnabled(t)
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
	logFilesBefore, _ := filepath.Glob(filepath.Join(cfg.DBDirectory, "logs_*.parquet"))

	// Reopen and manually prune
	store, err = NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)
	defer store.Close()

	cachedStore := store.(*cachedReceiptStore)
	parquetStore := cachedStore.backend.(*parquetReceiptStore)

	// Prune files before block 600 (should remove file 0)
	pruned := parquetStore.pruneOldFiles(600)

	// Count files after pruning
	receiptFilesAfter, _ := filepath.Glob(filepath.Join(cfg.DBDirectory, "receipts_*.parquet"))
	logFilesAfter, _ := filepath.Glob(filepath.Join(cfg.DBDirectory, "logs_*.parquet"))

	require.Greater(t, pruned, 0, "should have pruned at least one file")
	require.Less(t, len(receiptFilesAfter), len(receiptFilesBefore), "should have fewer receipt files")
	require.Less(t, len(logFilesAfter), len(logFilesBefore), "should have fewer log files")
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
			result := extractBlockNumber(tt.path)
			require.Equal(t, tt.expected, result)
		})
	}
}

// createMockParquetFile creates a minimal valid parquet file for testing
func createMockParquetFile(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	// Create an empty file - for testing file existence and naming only
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}
