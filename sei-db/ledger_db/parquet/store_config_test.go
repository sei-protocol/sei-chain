package parquet

import (
	"errors"
	"strings"
	"testing"

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
