package parquet

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pqgo "github.com/parquet-go/parquet-go"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
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
	store, err := NewStore(dbLogger.NewNopLogger(), StoreConfig{
		DBDirectory:        t.TempDir(),
		BlockFlushInterval: 7,
		MaxBlocksPerFile:   11,
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
	store, err := NewStore(dbLogger.NewNopLogger(), StoreConfig{
		DBDirectory: t.TempDir(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.Equal(t, defaultBlockFlushInterval, store.config.BlockFlushInterval)
	require.Equal(t, defaultMaxBlocksPerFile, store.config.MaxBlocksPerFile)
	require.Equal(t, defaultMaxBlocksPerFile, store.CacheRotateInterval())
}

func TestPruneOldFilesKeepsTrackingOnDeleteFailure(t *testing.T) {
	store, err := NewStore(dbLogger.NewNopLogger(), StoreConfig{
		DBDirectory: t.TempDir(),
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
	pruned := store.pruneOldFiles(600)
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

	store, err := NewStore(dbLogger.NewNopLogger(), StoreConfig{
		DBDirectory: dir,
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

func TestLazyInitCreatesFileOnFirstWrite(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(dbLogger.NewNopLogger(), StoreConfig{
		DBDirectory: dir,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// No parquet files should exist before any writes.
	receipts, _ := filepath.Glob(filepath.Join(dir, "receipts_*.parquet"))
	logs, _ := filepath.Glob(filepath.Join(dir, "logs_*.parquet"))
	require.Empty(t, receipts, "no receipt files should exist before first write")
	require.Empty(t, logs, "no log files should exist before first write")

	// Write a receipt at a high block number.
	input := ReceiptInput{
		BlockNumber: 5000,
		Receipt: ReceiptRecord{
			TxHash:       make([]byte, 32),
			BlockNumber:  5000,
			ReceiptBytes: []byte{0x1},
		},
	}
	store.mu.Lock()
	require.NoError(t, store.applyReceiptLocked(input))
	store.mu.Unlock()

	// The file should now exist with the correct block number in the name.
	receipts, _ = filepath.Glob(filepath.Join(dir, "receipts_*.parquet"))
	require.Len(t, receipts, 1)
	require.Contains(t, receipts[0], "receipts_5000.parquet")

	logs, _ = filepath.Glob(filepath.Join(dir, "logs_*.parquet"))
	require.Len(t, logs, 1)
	require.Contains(t, logs[0], "logs_5000.parquet")
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
