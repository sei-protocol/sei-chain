package parquet

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	pqgo "github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/require"
)

type mockParquetWAL struct {
	first       uint64
	last        uint64
	truncateErr error
}

func (m *mockParquetWAL) Write(_ WALEntry) error { return nil }

func (m *mockParquetWAL) TruncateBefore(_ uint64) error { return m.truncateErr }

func (m *mockParquetWAL) TruncateAfter(_ uint64) error { return nil }

func (m *mockParquetWAL) ReadAt(_ uint64) (WALEntry, error) { return WALEntry{}, nil }

func (m *mockParquetWAL) FirstOffset() (uint64, error) { return m.first, nil }

func (m *mockParquetWAL) LastOffset() (uint64, error) { return m.last, nil }

func (m *mockParquetWAL) Replay(_, _ uint64, _ func(index uint64, entry WALEntry) error) error {
	return nil
}

func (m *mockParquetWAL) Close() error { return nil }

func TestNewStoreAppliesConfiguredIntervals(t *testing.T) {
	store, err := NewStore(StoreConfig{
		DBDirectory:        t.TempDir(),
		BlockFlushInterval: 7,
		MaxBlocksPerFile:   11,
		TxIndexBackend:     "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.Equal(t, uint64(7), store.config.BlockFlushInterval)
	require.Equal(t, uint64(11), store.config.MaxBlocksPerFile)
	require.Equal(t, uint64(11), store.CacheRotateInterval())

	store.Reader.OnFileRotation(0)
	require.Len(t, store.Reader.GetFilesBeforeBlock(11), 1)
}

func TestNewStoreUsesDefaultIntervalsWhenUnset(t *testing.T) {
	store, err := NewStore(StoreConfig{
		DBDirectory: t.TempDir(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.Equal(t, defaultBlockFlushInterval, store.config.BlockFlushInterval)
	require.Equal(t, defaultMaxBlocksPerFile, store.config.MaxBlocksPerFile)
	require.Equal(t, defaultMaxBlocksPerFile, store.CacheRotateInterval())
	require.Equal(t, "pebbledb", store.config.TxIndexBackend)
}

func TestNewStorePreservesKeepRecentAndPruneIntervalSettings(t *testing.T) {
	store, err := NewStore(StoreConfig{
		DBDirectory:          t.TempDir(),
		KeepRecent:           123,
		PruneIntervalSeconds: 9,
		TxIndexBackend:       "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.Equal(t, int64(123), store.config.KeepRecent)
	require.Equal(t, int64(9), store.config.PruneIntervalSeconds)
	require.Equal(t, "none", store.config.TxIndexBackend)
}

func TestStoreEarliestVersionAccessors(t *testing.T) {
	store, err := NewStore(StoreConfig{
		DBDirectory:    t.TempDir(),
		TxIndexBackend: "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.Equal(t, int64(0), store.EarliestVersion())
	store.SetEarliestVersion(42)
	require.Equal(t, int64(42), store.EarliestVersion())
}

func TestNewStoreSucceedsWithTxIndexLookupEnabled(t *testing.T) {
	store, err := NewStore(StoreConfig{
		DBDirectory:    t.TempDir(),
		TxIndexBackend: "pebbledb",
	})
	require.NoError(t, err)
	require.NotNil(t, store)
	require.NoError(t, store.Close())
}

func TestPruneOldFilesKeepsTrackingOnDeleteFailure(t *testing.T) {
	store, err := NewStore(StoreConfig{
		DBDirectory:    t.TempDir(),
		TxIndexBackend: "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	store.Reader.OnFileRotation(0)
	require.Equal(t, 1, store.Reader.ClosedReceiptFileCount())

	origRemove := removeFile
	removeFile = func(path string) error {
		if strings.Contains(path, "receipts_0.parquet") {
			return errors.New("permission denied")
		}
		return nil
	}
	t.Cleanup(func() { removeFile = origRemove })

	// 0 + 500 <= 600, so file pair is prune-eligible.
	pruned := store.PruneOldFiles(600)
	require.Equal(t, 0, pruned)

	// Receipt file should remain tracked because delete failed.
	require.Equal(t, 1, store.Reader.ClosedReceiptFileCount())
}

func TestClearWALReturnsTruncateError(t *testing.T) {
	store := &Store{
		wal: &mockParquetWAL{
			first:       1,
			last:        2,
			truncateErr: errors.New("truncate failed"),
		},
	}

	err := store.ClearWAL()
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to truncate parquet WAL")
}

func TestCorruptLastFileDeletedOnStartup(t *testing.T) {
	dir := t.TempDir()

	// Create two valid parquet files and one corrupt file as the last.
	writeValidParquetFile(t, dir, "receipts_100.parquet")
	writeValidParquetFile(t, dir, "logs_100.parquet")
	writeValidParquetFile(t, dir, "receipts_600.parquet")
	writeValidParquetFile(t, dir, "logs_600.parquet")

	corruptReceipt := filepath.Join(dir, "receipts_1100.parquet")
	require.NoError(t, os.WriteFile(corruptReceipt, []byte("not a parquet file"), 0o644))
	corruptLog := filepath.Join(dir, "logs_1100.parquet")
	require.NoError(t, os.WriteFile(corruptLog, []byte("not a parquet file"), 0o644))

	store, err := NewStore(StoreConfig{
		DBDirectory:    dir,
		TxIndexBackend: "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Corrupt files should be deleted from disk.
	_, err = os.Stat(corruptReceipt)
	require.True(t, os.IsNotExist(err), "corrupt receipt file should have been deleted")
	_, err = os.Stat(corruptLog)
	require.True(t, os.IsNotExist(err), "corrupt log file should have been deleted")

	// Valid files should still be tracked.
	require.Equal(t, 2, store.Reader.ClosedReceiptFileCount())
}

func TestCorruptLogFileUntracksReceiptCounterpart(t *testing.T) {
	dir := t.TempDir()

	// Valid receipt+log pair at block 100.
	writeValidParquetFile(t, dir, "receipts_100.parquet")
	writeValidParquetFile(t, dir, "logs_100.parquet")

	// Valid receipt at block 600 but CORRUPT log at block 600.
	// This is the bug scenario: receipts are scanned first and receipts_600
	// is tracked. Then logs are scanned, logs_600 is corrupt, and its
	// counterpart receipts_600 is deleted from disk. Without the fix the
	// reader would still track the now-missing receipts_600.
	writeValidParquetFile(t, dir, "receipts_600.parquet")
	corruptLog := filepath.Join(dir, "logs_600.parquet")
	require.NoError(t, os.WriteFile(corruptLog, []byte("not a parquet file"), 0o644))

	store, err := NewStore(StoreConfig{
		DBDirectory:    dir,
		TxIndexBackend: "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Both the corrupt log and its valid receipt counterpart should be gone.
	_, err = os.Stat(filepath.Join(dir, "receipts_600.parquet"))
	require.True(t, os.IsNotExist(err), "receipt counterpart should have been deleted")
	_, err = os.Stat(corruptLog)
	require.True(t, os.IsNotExist(err), "corrupt log file should have been deleted")

	// Only the block-100 pair should remain tracked.
	require.Equal(t, 1, store.Reader.ClosedReceiptFileCount())
}

func TestLazyInitCreatesFileOnFirstWrite(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 500,
		TxIndexBackend:   "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// No parquet files should exist before any writes.
	receipts, _ := filepath.Glob(filepath.Join(dir, "receipts_*.parquet"))
	logs, _ := filepath.Glob(filepath.Join(dir, "logs_*.parquet"))
	require.Empty(t, receipts, "no receipt files should exist before first write")
	require.Empty(t, logs, "no log files should exist before first write")

	// First receipt off a boundary: filename must snap to the interval start so
	// reader pruning/range math stays consistent with rotation boundaries.
	input := ReceiptInput{
		BlockNumber: 5234,
		Receipt: ReceiptRecord{
			TxHash:       make([]byte, 32),
			BlockNumber:  5234,
			ReceiptBytes: []byte{0x1},
		},
	}
	store.mu.Lock()
	require.NoError(t, store.applyReceiptLocked(input))
	store.mu.Unlock()

	// The file should now exist with the aligned block number in the name.
	receipts, _ = filepath.Glob(filepath.Join(dir, "receipts_*.parquet"))
	require.Len(t, receipts, 1)
	require.Contains(t, receipts[0], "receipts_5000.parquet")

	logs, _ = filepath.Glob(filepath.Join(dir, "logs_*.parquet"))
	require.Len(t, logs, 1)
	require.Contains(t, logs[0], "logs_5000.parquet")
}

// TestLazyInitAlignedFirstFilePruneEligibility guards against using the raw
// first receipt height as the parquet filename start: the reader assumes each
// file covers [start, start+MaxBlocksPerFile). A misaligned name (e.g.
// receipts_1234.parquet) makes GetFilesBeforeBlock think the file still holds
// blocks through 1733 and delays pruning until pruneBeforeBlock exceeds that.
func TestLazyInitAlignedFirstFilePruneEligibility(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 500,
		TxIndexBackend:   "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	receipt := ReceiptRecord{
		TxHash:       make([]byte, 32),
		ReceiptBytes: []byte{0x1},
	}

	require.NoError(t, store.WriteReceipts([]ReceiptInput{{
		BlockNumber: 1234,
		Receipt: ReceiptRecord{
			TxHash:       receipt.TxHash,
			BlockNumber:  1234,
			ReceiptBytes: receipt.ReceiptBytes,
		},
		ReceiptBytes: receipt.ReceiptBytes,
	}}))

	require.NoError(t, store.WriteReceipts([]ReceiptInput{{
		BlockNumber: 1500,
		Receipt: ReceiptRecord{
			TxHash:       receipt.TxHash,
			BlockNumber:  1500,
			ReceiptBytes: receipt.ReceiptBytes,
		},
		ReceiptBytes: receipt.ReceiptBytes,
	}}))

	// First segment is [1000, 1500) in naming; all rows are < 1500, so it is
	// eligible when pruning everything strictly before block 1500.
	pairs := store.Reader.GetFilesBeforeBlock(1500)
	require.Len(t, pairs, 1, "misaligned lazy-init filenames would leave zero prune-eligible files here")
	require.Equal(t, uint64(1000), pairs[0].StartBlock)
}

func TestObserveEmptyBlockHonorsMonotonicLastSeen(t *testing.T) {
	store, err := NewStore(StoreConfig{
		DBDirectory:      t.TempDir(),
		MaxBlocksPerFile: 500,
		TxIndexBackend:   "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ObserveEmptyBlock(100))
	require.Equal(t, uint64(100), store.lastSeenBlock)

	// Stale / out-of-order height must not regress lastSeenBlock.
	require.NoError(t, store.ObserveEmptyBlock(50))
	require.Equal(t, uint64(100), store.lastSeenBlock)

	require.NoError(t, store.ObserveEmptyBlock(100))
	require.Equal(t, uint64(100), store.lastSeenBlock)

	require.NoError(t, store.ObserveEmptyBlock(200))
	require.Equal(t, uint64(200), store.lastSeenBlock)
}

// TestReopenLazyInitPreservesAlignedBoundaryFile guards against truncating a
// closed parquet file on the first write after reopen. When the last committed
// block coincides with (or falls inside) a rotation window whose aligned file
// already exists on disk (e.g. receipts_10.parquet holds block 10 after a
// clean rotation+close), lazy init must NOT re-snap fileStartBlock back to the
// aligned value — doing so would os.Create the existing file and wipe out all
// prior data in that window.
func TestReopenLazyInitPreservesAlignedBoundaryFile(t *testing.T) {
	dir := t.TempDir()
	cfg := StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 10,
		TxIndexBackend:   "none",
	}

	mkInput := func(block uint64) ReceiptInput {
		var txHash [32]byte
		binary.BigEndian.PutUint64(txHash[24:], block)
		return ReceiptInput{
			BlockNumber: block,
			Receipt: ReceiptRecord{
				TxHash:       txHash[:],
				BlockNumber:  block,
				ReceiptBytes: []byte{byte(block)},
			},
			ReceiptBytes: []byte{byte(block)},
		}
	}

	store, err := NewStore(cfg)
	require.NoError(t, err)

	// Blocks 1..9 land in receipts_0.parquet; block 10 rotates and lands as
	// the sole entry in receipts_10.parquet, then we close cleanly so both
	// files have valid footers.
	for block := uint64(1); block <= 10; block++ {
		require.NoError(t, store.WriteReceipts([]ReceiptInput{mkInput(block)}))
	}
	require.NoError(t, store.Close())

	alignedFile := filepath.Join(dir, "receipts_10.parquet")
	infoBefore, err := os.Stat(alignedFile)
	require.NoError(t, err)
	require.Positive(t, infoBefore.Size(), "receipts_10.parquet should exist with data")

	// Reopen and write block 11. Pre-fix, applyReceiptLocked would re-align
	// fileStartBlock from 11 down to 10 and os.Create receipts_10.parquet,
	// destroying block 10's data.
	store, err = NewStore(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.WriteReceipts([]ReceiptInput{mkInput(11)}))

	infoAfter, err := os.Stat(alignedFile)
	require.NoError(t, err)
	require.GreaterOrEqual(t, infoAfter.Size(), infoBefore.Size(),
		"reopen lazy-init must not truncate the on-disk aligned-boundary file")

	var block10Hash common.Hash
	binary.BigEndian.PutUint64(block10Hash[24:], 10)
	result, err := store.GetReceiptByTxHash(context.Background(), block10Hash)
	require.NoError(t, err)
	require.NotNil(t, result, "block 10 must remain queryable after reopen+write")
	require.Equal(t, uint64(10), result.BlockNumber)

	maxBlock, ok, err := store.Reader.MaxReceiptBlockNumber(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(10), maxBlock,
		"closed files should still report block 10 as the max")
}

// TestReopenLazyInitUsesAlignedStartOnGap ensures the reopen-preservation
// logic does not over-fire: if the first post-reopen write lands in a fresh
// rotation window (past any file that could be clobbered), lazy init must
// still snap down to the aligned boundary so reader range math stays correct.
func TestReopenLazyInitUsesAlignedStartOnGap(t *testing.T) {
	dir := t.TempDir()
	cfg := StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 10,
		TxIndexBackend:   "none",
	}

	mkInput := func(block uint64) ReceiptInput {
		var txHash [32]byte
		binary.BigEndian.PutUint64(txHash[24:], block)
		return ReceiptInput{
			BlockNumber: block,
			Receipt: ReceiptRecord{
				TxHash:       txHash[:],
				BlockNumber:  block,
				ReceiptBytes: []byte{byte(block)},
			},
			ReceiptBytes: []byte{byte(block)},
		}
	}

	store, err := NewStore(cfg)
	require.NoError(t, err)
	for block := uint64(1); block <= 10; block++ {
		require.NoError(t, store.WriteReceipts([]ReceiptInput{mkInput(block)}))
	}
	require.NoError(t, store.Close())

	store, err = NewStore(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// First post-reopen write is in a new rotation window (>= 20).
	require.NoError(t, store.WriteReceipts([]ReceiptInput{mkInput(25)}))

	_, err = os.Stat(filepath.Join(dir, "receipts_20.parquet"))
	require.NoError(t, err, "aligned filename should be used when the first post-reopen write is past the prior window")
}

func writeValidParquetFile(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	require.NoError(t, err)

	type minimalRecord struct {
		BlockNumber uint64 `parquet:"block_number"`
	}
	writer := pqgo.NewGenericWriter[minimalRecord](f)
	_, err = writer.Write([]minimalRecord{{BlockNumber: 0}})
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, f.Close())
}
