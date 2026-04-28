package parquet_v2

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	parquetgo "github.com/parquet-go/parquet-go"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func TestNewStoreCreatesDirectoryAndClosesIdempotently(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "parquet")

	store, err := NewStore(parquet.StoreConfig{DBDirectory: dir})
	require.NoError(t, err)
	require.DirExists(t, dir)
	require.DirExists(t, filepath.Join(dir, "parquet-wal"))

	require.NoError(t, store.Flush())
	require.NoError(t, store.Close())
	require.NoError(t, store.Close())
}

func TestNewStoreSeedsLatestVersionFromClosedFiles(t *testing.T) {
	dir := t.TempDir()
	writeReceiptFile(t, dir, 100, []uint64{101, 123})
	writeLogFile(t, dir, 100)

	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 100,
	})
	require.NoError(t, err)
	require.Equal(t, int64(123), store.LatestVersion())
	require.Equal(t, uint64(124), store.FileStartBlock())
	require.NoError(t, store.Close())

	reopened, err := NewStore(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 100,
	})
	require.NoError(t, err)
	require.Equal(t, int64(123), reopened.LatestVersion())
	require.Equal(t, uint64(124), reopened.FileStartBlock())
	require.NoError(t, reopened.Close())
}

func TestNewStoreRemovesCorruptTrailingPair(t *testing.T) {
	dir := t.TempDir()
	writeReceiptFile(t, dir, 0, []uint64{1})
	writeLogFile(t, dir, 0)

	corruptReceipt := filepath.Join(dir, "receipts_500.parquet")
	require.NoError(t, os.WriteFile(corruptReceipt, []byte("not parquet"), 0o644))
	corruptLog := filepath.Join(dir, "logs_500.parquet")
	require.NoError(t, os.WriteFile(corruptLog, []byte("not parquet"), 0o644))

	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 500,
	})
	require.NoError(t, err)
	require.NoError(t, store.Close())

	_, err = os.Stat(corruptReceipt)
	require.True(t, os.IsNotExist(err), "corrupt receipt file should be deleted")
	_, err = os.Stat(corruptLog)
	require.True(t, os.IsNotExist(err), "corrupt log file should be deleted")
}

func TestNewStoreRemovesReceiptCounterpartForCorruptTrailingLog(t *testing.T) {
	dir := t.TempDir()
	writeReceiptFile(t, dir, 0, []uint64{1})
	writeLogFile(t, dir, 0)
	writeReceiptFile(t, dir, 500, []uint64{501})

	corruptLog := filepath.Join(dir, "logs_500.parquet")
	require.NoError(t, os.WriteFile(corruptLog, []byte("not parquet"), 0o644))
	receiptCounterpart := filepath.Join(dir, "receipts_500.parquet")

	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 500,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), store.LatestVersion())
	require.NoError(t, store.Close())

	_, err = os.Stat(receiptCounterpart)
	require.True(t, os.IsNotExist(err), "receipt counterpart should be deleted")
	_, err = os.Stat(corruptLog)
	require.True(t, os.IsNotExist(err), "corrupt log file should be deleted")
}

func TestNewStoreIgnoresUnmatchedFiles(t *testing.T) {
	dir := t.TempDir()
	writeReceiptFile(t, dir, 0, []uint64{1})
	writeLogFile(t, dir, 500)

	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 500,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), store.LatestVersion())
	require.Equal(t, uint64(0), store.FileStartBlock())
	require.NoError(t, store.Close())
}

func TestScanClosedFilesSortsByStartBlock(t *testing.T) {
	dir := t.TempDir()
	for _, startBlock := range []uint64{1000, 0, 500} {
		writeReceiptFile(t, dir, startBlock, []uint64{startBlock + 1})
		writeLogFile(t, dir, startBlock)
	}

	reader, err := NewReaderWithMaxBlocksPerFile(dir, 500)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	closedFiles, err := scanClosedFiles(dir, reader)
	require.NoError(t, err)
	require.Len(t, closedFiles, 3)
	require.Equal(t, uint64(0), closedFiles[0].startBlock)
	require.Equal(t, uint64(500), closedFiles[1].startBlock)
	require.Equal(t, uint64(1000), closedFiles[2].startBlock)
}

func writeReceiptFile(t *testing.T, dir string, startBlock uint64, blocks []uint64) {
	t.Helper()

	path := filepath.Join(dir, fmt.Sprintf("receipts_%d.parquet", startBlock))
	f, err := os.Create(path)
	require.NoError(t, err)

	w := parquetgo.NewGenericWriter[parquet.ReceiptRecord](f)
	for _, block := range blocks {
		txHash := common.BigToHash(new(big.Int).SetUint64(block))
		_, err := w.Write([]parquet.ReceiptRecord{{
			TxHash:       txHash[:],
			BlockNumber:  block,
			ReceiptBytes: []byte{byte(block)},
		}})
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
}

func writeLogFile(t *testing.T, dir string, startBlock uint64) {
	t.Helper()

	path := filepath.Join(dir, fmt.Sprintf("logs_%d.parquet", startBlock))
	f, err := os.Create(path)
	require.NoError(t, err)

	w := parquetgo.NewGenericWriter[parquet.LogRecord](f)
	txHash := common.BigToHash(new(big.Int).SetUint64(startBlock))
	_, err = w.Write([]parquet.LogRecord{{
		BlockNumber: startBlock,
		TxHash:      txHash[:],
	}})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
}
