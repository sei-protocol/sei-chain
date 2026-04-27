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
		TxIndexBackend:   "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	receipt := ReceiptRecord{
		TxHash:       make([]byte, 32),
		BlockNumber:  1,
		ReceiptBytes: make([]byte, 512),
	}

	// Write blocks 1-9 (below the first boundary at block 10, no rotation yet).
	for block := uint64(1); block <= 9; block++ {
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

	// Block 10 is aligned to MaxBlocksPerFile=10, so it triggers rotation,
	// which calls ClearWAL() before the new block's WAL entry is written.
	receipt.BlockNumber = 10
	require.NoError(t, store.WriteReceipts([]ReceiptInput{{
		BlockNumber:  10,
		Receipt:      receipt,
		ReceiptBytes: receipt.ReceiptBytes,
	}}))

	sizeAfterRotation := walDirSize(t, walDir)

	// After ClearWAL the WAL should contain at most the single entry from
	// block 10. The pre-rotation data (blocks 1-9) must be gone.
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
		TxIndexBackend:   "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	receipt := ReceiptRecord{
		TxHash:       make([]byte, 32),
		BlockNumber:  1,
		ReceiptBytes: make([]byte, 1024),
	}

	walDir := filepath.Join(dir, "parquet-wal")

	// Write blocks 1-4 (below the first boundary at 5, no rotation yet). The
	// WAL holds all 4 entries.
	for block := uint64(1); block <= 4; block++ {
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

	// Write blocks 5-25 → 5 aligned rotations at 5, 10, 15, 20, 25.
	for block := uint64(5); block <= 25; block++ {
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
