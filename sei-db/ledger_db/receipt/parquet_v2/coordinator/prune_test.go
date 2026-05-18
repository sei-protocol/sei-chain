package coordinator

import (
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func TestPruneTickDeletesEligibleClosedFiles(t *testing.T) {
	dir := t.TempDir()
	closedFiles := writeClosedFileSet(t, dir, 0, 4, 8)

	reader, err := NewReaderWithMaxBlocksPerFile(dir, 4)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })

	coord := &Coordinator{
		config: parquet.StoreConfig{
			KeepRecent:       4,
			MaxBlocksPerFile: 4,
		},
		closedFiles:   closedFiles,
		latestVersion: 12,
		reader:        reader,
	}

	forcePruneTickForTest(coord)

	require.Len(t, coord.closedFiles, 1)
	require.Equal(t, uint64(8), coord.closedFiles[0].startBlock)
	require.NoFileExists(t, filepath.Join(dir, "receipts_0.parquet"))
	require.NoFileExists(t, filepath.Join(dir, "logs_0.parquet"))
	require.NoFileExists(t, filepath.Join(dir, "receipts_4.parquet"))
	require.NoFileExists(t, filepath.Join(dir, "logs_4.parquet"))
	require.FileExists(t, filepath.Join(dir, "receipts_8.parquet"))
	require.FileExists(t, filepath.Join(dir, "logs_8.parquet"))

	prunedResult := readClosedReceiptForTest(t, coord, common.BigToHash(new(big.Int).SetUint64(1)), 1)
	require.Nil(t, prunedResult)

	keptResult := readClosedReceiptForTest(t, coord, common.BigToHash(new(big.Int).SetUint64(9)), 9)
	require.NotNil(t, keptResult)
	require.Equal(t, uint64(9), keptResult.BlockNumber)
}

func TestPruneKeepsFilePairTrackedWhenReceiptDeleteFails(t *testing.T) {
	dir := t.TempDir()
	closedFiles := writeClosedFileSet(t, dir, 0)
	receiptPath := filepath.Join(dir, "receipts_0.parquet")
	logPath := filepath.Join(dir, "logs_0.parquet")

	originalRemoveFile := removeFile
	t.Cleanup(func() { removeFile = originalRemoveFile })
	removeFile = func(path string) error {
		if path == receiptPath {
			return errors.New("delete failed")
		}
		return os.Remove(path)
	}

	coord := &Coordinator{
		config:      parquet.StoreConfig{MaxBlocksPerFile: 4},
		closedFiles: closedFiles,
	}

	require.Zero(t, coord.pruneOldFiles(4))
	require.Len(t, coord.closedFiles, 1)
	require.Equal(t, uint64(0), coord.closedFiles[0].startBlock)
	require.Equal(t, receiptPath, coord.closedFiles[0].receiptPath,
		"receipt path must remain set so the next prune tick retries")
	require.Empty(t, coord.closedFiles[0].logPath,
		"log path must be cleared after a successful delete to avoid handing a missing file to DuckDB")
	require.FileExists(t, receiptPath)
	require.NoFileExists(t, logPath)
	require.Empty(t, coord.logFilesSnapshot(),
		"snapshot must not expose the deleted log path")
	require.Equal(t, []string{receiptPath}, coord.receiptFilesSnapshot(),
		"snapshot must still expose the surviving receipt path")
}

// TestPruneDropsReceiptPathWhenLogDeleteFails covers Bug 2: when the log
// delete fails after the receipt was already removed, the entry must no
// longer expose the now-deleted receipt path via the snapshot helpers.
func TestPruneDropsReceiptPathWhenLogDeleteFails(t *testing.T) {
	dir := t.TempDir()
	closedFiles := writeClosedFileSet(t, dir, 0)
	receiptPath := filepath.Join(dir, "receipts_0.parquet")
	logPath := filepath.Join(dir, "logs_0.parquet")

	originalRemoveFile := removeFile
	t.Cleanup(func() { removeFile = originalRemoveFile })
	removeFile = func(path string) error {
		if path == logPath {
			return errors.New("delete failed")
		}
		return os.Remove(path)
	}

	coord := &Coordinator{
		config:      parquet.StoreConfig{MaxBlocksPerFile: 4},
		closedFiles: closedFiles,
	}

	require.Zero(t, coord.pruneOldFiles(4))
	require.Len(t, coord.closedFiles, 1,
		"entry must remain tracked so the surviving log file is retried on the next tick")
	require.Equal(t, uint64(0), coord.closedFiles[0].startBlock)
	require.Empty(t, coord.closedFiles[0].receiptPath,
		"receipt path must be cleared so the snapshot does not hand a deleted file to DuckDB")
	require.Equal(t, logPath, coord.closedFiles[0].logPath,
		"log path must remain set so the next prune tick retries")
	require.NoFileExists(t, receiptPath)
	require.FileExists(t, logPath)
	require.Empty(t, coord.receiptFilesSnapshot(),
		"snapshot must not expose the deleted receipt path")
	require.Empty(t, coord.receiptFileSnapshotForBlock(1),
		"per-block snapshot must not return a deleted receipt path")
	require.Equal(t, []string{logPath}, coord.logFilesSnapshot(),
		"snapshot must still expose the surviving log path")
}

// TestPruneRetrySucceedsAfterTransientFailure drives two ticks: the first
// fails the log delete, the second succeeds. The entry must finally be
// dropped from c.closedFiles and counted exactly once as pruned.
func TestPruneRetrySucceedsAfterTransientFailure(t *testing.T) {
	dir := t.TempDir()
	closedFiles := writeClosedFileSet(t, dir, 0)
	logPath := filepath.Join(dir, "logs_0.parquet")

	originalRemoveFile := removeFile
	t.Cleanup(func() { removeFile = originalRemoveFile })
	failLog := true
	removeFile = func(path string) error {
		if path == logPath && failLog {
			return errors.New("transient delete failed")
		}
		return os.Remove(path)
	}

	coord := &Coordinator{
		config:      parquet.StoreConfig{MaxBlocksPerFile: 4},
		closedFiles: closedFiles,
	}

	require.Zero(t, coord.pruneOldFiles(4), "first tick must not count an unfinished prune")
	require.Len(t, coord.closedFiles, 1)
	require.Empty(t, coord.closedFiles[0].receiptPath)
	require.Equal(t, logPath, coord.closedFiles[0].logPath)

	failLog = false
	require.Equal(t, 1, coord.pruneOldFiles(4),
		"second tick must count the now-complete prune exactly once")
	require.Empty(t, coord.closedFiles, "entry must be dropped once both paths are cleared")
	require.NoFileExists(t, logPath)
}
