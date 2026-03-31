package parquet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// walDirSize returns the total size of all files in dir (non-recursive).
func walDirSize(t *testing.T, dir string) int64 {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		require.NoError(t, err)
		total += info.Size()
	}
	return total
}

// TestClearWALActuallyFreesSpace uses the real tidwall/wal-backed WAL (not a
// mock) to verify that ClearWAL genuinely removes data from disk. This catches
// the bug where AllowEmpty=false caused TruncateFront to silently fail with
// ErrOutOfRange, leaving every WAL entry on disk forever.
func TestClearWALActuallyFreesSpace(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	receipt := ReceiptRecord{
		TxHash:       make([]byte, 32),
		BlockNumber:  1,
		ReceiptBytes: make([]byte, 512),
	}

	// Write 10 blocks (fills one file, no rotation yet).
	for block := uint64(1); block <= 10; block++ {
		receipt.BlockNumber = block
		require.NoError(t, store.WriteReceipts([]ReceiptInput{{
			BlockNumber:  block,
			Receipt:      receipt,
			ReceiptBytes: receipt.ReceiptBytes,
		}}))
	}

	walDir := filepath.Join(dir, "parquet-wal")
	sizeBeforeRotation := walDirSize(t, walDir)
	require.Greater(t, sizeBeforeRotation, int64(0), "WAL should have data before rotation")

	// Block 11 triggers rotation (blocksInFile=10 >= MaxBlocksPerFile=10),
	// which calls ClearWAL().
	receipt.BlockNumber = 11
	require.NoError(t, store.WriteReceipts([]ReceiptInput{{
		BlockNumber:  11,
		Receipt:      receipt,
		ReceiptBytes: receipt.ReceiptBytes,
	}}))

	sizeAfterRotation := walDirSize(t, walDir)

	// After ClearWAL the WAL should contain at most the single entry from
	// block 11. The pre-rotation data (blocks 1-10) must be gone.
	require.Less(t, sizeAfterRotation, sizeBeforeRotation,
		"WAL should shrink after rotation; ClearWAL may not be truncating (AllowEmpty bug)")
}

// TestClearWALEmptiesAfterMultipleRotations writes enough blocks to trigger
// several file rotations and verifies the WAL stays bounded rather than
// growing monotonically.
func TestClearWALEmptiesAfterMultipleRotations(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 5,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	receipt := ReceiptRecord{
		TxHash:       make([]byte, 32),
		BlockNumber:  1,
		ReceiptBytes: make([]byte, 1024),
	}

	walDir := filepath.Join(dir, "parquet-wal")

	// Write 5 blocks to fill one file (no rotation yet). The WAL should
	// hold all 5 entries at this point.
	for block := uint64(1); block <= 5; block++ {
		receipt.BlockNumber = block
		require.NoError(t, store.WriteReceipts([]ReceiptInput{{
			BlockNumber:  block,
			Receipt:      receipt,
			ReceiptBytes: receipt.ReceiptBytes,
		}}))
	}
	sizeBeforeAnyRotation := walDirSize(t, walDir)
	require.Greater(t, sizeBeforeAnyRotation, int64(0),
		"WAL should have data before first rotation")

	// Write 20 more blocks (blocks 6-25) → 4 more rotations.
	for block := uint64(6); block <= 25; block++ {
		receipt.BlockNumber = block
		require.NoError(t, store.WriteReceipts([]ReceiptInput{{
			BlockNumber:  block,
			Receipt:      receipt,
			ReceiptBytes: receipt.ReceiptBytes,
		}}))
	}

	sizeAtEnd := walDirSize(t, walDir)

	// Without truncation the WAL would hold all 25 blocks (~5x the initial
	// size). With working truncation it should be no larger than one
	// rotation window.
	require.Less(t, sizeAtEnd, sizeBeforeAnyRotation,
		"WAL should not grow across rotations; ClearWAL is not reclaiming space")
}
