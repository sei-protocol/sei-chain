package parquet_v2

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func TestLazyInitUsesAlignedStartForFirstOffBoundaryWrite(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 500,
	})
	require.NoError(t, err)

	require.NoError(t, store.WriteReceipts([]parquet.ReceiptInput{
		testReceiptInput(5234, common.HexToHash("0x5234")),
	}))
	require.NoError(t, store.Close())

	require.FileExists(t, filepath.Join(dir, "receipts_5000.parquet"))
	require.FileExists(t, filepath.Join(dir, "logs_5000.parquet"))
}

func TestReopenLazyInitPreservesExistingAlignedFile(t *testing.T) {
	dir := t.TempDir()
	writeReceiptFile(t, dir, 10, []uint64{10})
	writeLogFile(t, dir, 10)

	alignedFile := filepath.Join(dir, "receipts_10.parquet")
	infoBefore, err := os.Stat(alignedFile)
	require.NoError(t, err)

	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 10,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(11), store.FileStartBlock())

	require.NoError(t, store.WriteReceipts([]parquet.ReceiptInput{
		testReceiptInput(11, common.HexToHash("0x11")),
	}))
	require.NoError(t, store.Close())

	infoAfter, err := os.Stat(alignedFile)
	require.NoError(t, err)
	require.Equal(t, infoBefore.Size(), infoAfter.Size(), "existing aligned file must not be truncated")
	require.FileExists(t, filepath.Join(dir, "receipts_11.parquet"))
}

func TestReopenLazyInitUsesAlignedStartOnGap(t *testing.T) {
	dir := t.TempDir()
	writeReceiptFile(t, dir, 10, []uint64{10})
	writeLogFile(t, dir, 10)

	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 10,
	})
	require.NoError(t, err)

	require.NoError(t, store.WriteReceipts([]parquet.ReceiptInput{
		testReceiptInput(25, common.HexToHash("0x25")),
	}))
	require.NoError(t, store.Close())

	require.FileExists(t, filepath.Join(dir, "receipts_20.parquet"))
	require.FileExists(t, filepath.Join(dir, "logs_20.parquet"))
}
