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
	bootstrapWorkersForTest(coord)
	t.Cleanup(func() { coord.shutdownWorkers() })

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

func TestPruneKeepsFilePairTrackedWhenDeleteFails(t *testing.T) {
	dir := t.TempDir()
	closedFiles := writeClosedFileSet(t, dir, 0)
	failPath := filepath.Join(dir, "receipts_0.parquet")

	originalRemoveFile := removeFile
	t.Cleanup(func() { removeFile = originalRemoveFile })
	removeFile = func(path string) error {
		if path == failPath {
			return errors.New("delete failed")
		}
		return os.Remove(path)
	}

	coord := &Coordinator{
		config:        parquet.StoreConfig{KeepRecent: 4, MaxBlocksPerFile: 4},
		closedFiles:   closedFiles,
		latestVersion: 8,
	}
	bootstrapWorkersForTest(coord)
	t.Cleanup(func() { coord.shutdownWorkers() })

	forcePruneTickForTest(coord)

	require.Len(t, coord.closedFiles, 1)
	require.Equal(t, uint64(0), coord.closedFiles[0].startBlock)
	require.FileExists(t, failPath)
	require.FileExists(t, filepath.Join(dir, "logs_0.parquet"))
}

// Regression: a failed prune for an older file must be reinserted in sorted
// position so receiptFileSnapshotForBlock's early-break still finds it.
func TestPruneFailureReinsertsInSortedOrder(t *testing.T) {
	dir := t.TempDir()
	closedFiles := writeClosedFileSet(t, dir, 0, 4, 8)
	failPath := filepath.Join(dir, "receipts_0.parquet")

	originalRemoveFile := removeFile
	t.Cleanup(func() { removeFile = originalRemoveFile })
	removeFile = func(path string) error {
		if path == failPath {
			return errors.New("delete failed")
		}
		return os.Remove(path)
	}

	reader, err := NewReaderWithMaxBlocksPerFile(dir, 4)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })

	coord := &Coordinator{
		config:        parquet.StoreConfig{KeepRecent: 4, MaxBlocksPerFile: 4},
		closedFiles:   closedFiles,
		latestVersion: 12,
		reader:        reader,
	}
	bootstrapWorkersForTest(coord)
	t.Cleanup(func() { coord.shutdownWorkers() })

	forcePruneTickForTest(coord)

	// File 4 deleted; file 0 reinserted after delete failure; file 8 retained.
	// Ordering matters: file 0 must come before file 8 so a query for block 1
	// can still resolve to receipts_0.parquet.
	require.Len(t, coord.closedFiles, 2)
	require.Equal(t, uint64(0), coord.closedFiles[0].startBlock)
	require.Equal(t, uint64(8), coord.closedFiles[1].startBlock)

	result := readClosedReceiptForTest(t, coord, common.BigToHash(new(big.Int).SetUint64(1)), 1)
	require.NotNil(t, result)
	require.Equal(t, uint64(1), result.BlockNumber)
}
