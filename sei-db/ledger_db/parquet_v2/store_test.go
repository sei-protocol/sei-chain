package parquet_v2

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func makeReceipt(block uint64) ReceiptInput {
	txHash := common.BigToHash(new(big.Int).SetUint64(block))
	rec := ReceiptRecord{
		TxHash:       CopyBytes(txHash[:]),
		BlockNumber:  block,
		ReceiptBytes: []byte{0x1, 0x2, 0x3},
	}
	return ReceiptInput{
		BlockNumber:  block,
		Receipt:      rec,
		ReceiptBytes: rec.ReceiptBytes,
	}
}

// TestStoreOpenWriteReadClose covers the basic happy path through the
// coordinator: open, write a few blocks, rotate, read back, close.
func TestStoreOpenWriteReadClose(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 5,
		TxIndexBackend:   "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Write blocks 1..5. Block 5 is the rotation boundary, so the file
	// receipts_0.parquet covers [0, 5) — i.e. blocks 1..4 — and writing block
	// 5 rotates and starts receipts_5.parquet.
	for block := uint64(1); block <= 9; block++ {
		require.NoError(t, store.WriteReceipts([]ReceiptInput{makeReceipt(block)}))
	}

	// Block 5 should now live in the closed file receipts_0.parquet.
	ctx := context.Background()
	txHash := common.BigToHash(new(big.Int).SetUint64(3))
	result, err := store.GetReceiptByTxHash(ctx, txHash)
	require.NoError(t, err)
	require.NotNil(t, result, "block 3 should be readable from the rotated parquet file")
	require.Equal(t, uint64(3), result.BlockNumber)

	// Block 7 is still in the open writer's buffer (not yet readable).
	openTxHash := common.BigToHash(new(big.Int).SetUint64(7))
	openResult, err := store.GetReceiptByTxHash(ctx, openTxHash)
	require.NoError(t, err)
	require.Nil(t, openResult, "block 7 lives in the open file and is not yet readable")
}

// TestStoreRotationCreatesParquetFile verifies that crossing a rotation
// boundary closes the active parquet file and writes a new one.
func TestStoreRotationCreatesParquetFile(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 3,
		TxIndexBackend:   "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	for block := uint64(1); block <= 6; block++ {
		require.NoError(t, store.WriteReceipts([]ReceiptInput{makeReceipt(block)}))
	}

	// After writing block 3 and block 6 we should have rotated twice — files
	// receipts_0 and receipts_3 must exist, plus the open receipts_6.
	for _, start := range []uint64{0, 3, 6} {
		path := filepath.Join(dir, "receipts_"+formatBlockForTest(start)+".parquet")
		_, err := os.Stat(path)
		require.NoError(t, err, "expected parquet file at start=%d", start)
	}
}

// TestStoreObserveEmptyBlockRotates ensures empty blocks at boundaries don't
// stall rotation.
func TestStoreObserveEmptyBlockRotates(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 5,
		TxIndexBackend:   "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Write block 1 to initialize the writer.
	require.NoError(t, store.WriteReceipts([]ReceiptInput{makeReceipt(1)}))

	// Observe an empty boundary block. This should rotate into a new file.
	require.NoError(t, store.ObserveEmptyBlock(5))

	// Write block 6 — it should land in the new file (start block 5).
	require.NoError(t, store.WriteReceipts([]ReceiptInput{makeReceipt(6)}))

	// receipts_0.parquet must exist and be readable.
	path0 := filepath.Join(dir, "receipts_0.parquet")
	_, err = os.Stat(path0)
	require.NoError(t, err, "expected receipts_0.parquet after empty-block rotation")
}

// TestStoreClearWALShrinks verifies that rotation truncates the WAL.
func TestStoreClearWALShrinks(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 10,
		TxIndexBackend:   "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	for block := uint64(1); block <= 9; block++ {
		require.NoError(t, store.WriteReceipts([]ReceiptInput{makeReceipt(block)}))
	}

	walDir := filepath.Join(dir, "parquet-wal")
	sizeBefore := dirSize(t, walDir)
	require.Greater(t, sizeBefore, int64(0))

	// Rotation boundary: triggers ClearWAL.
	require.NoError(t, store.WriteReceipts([]ReceiptInput{makeReceipt(10)}))

	sizeAfter := dirSize(t, walDir)
	require.Less(t, sizeAfter, sizeBefore, "WAL should shrink after rotation")
}

// TestStorePruneOldFiles deletes files entirely below the prune threshold.
func TestStorePruneOldFiles(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 5,
		TxIndexBackend:   "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Produce three closed files (start blocks 0, 5, 10) and an open one at 15.
	for block := uint64(1); block <= 16; block++ {
		require.NoError(t, store.WriteReceipts([]ReceiptInput{makeReceipt(block)}))
	}

	// Files entirely below block 10 should be pruneable: that's just receipts_0
	// (covers [0,5)) and receipts_5 (covers [5,10)).
	pruned := store.PruneOldFiles(10)
	require.Equal(t, 2, pruned, "expected 2 file pairs pruned")

	for _, start := range []uint64{0, 5} {
		path := filepath.Join(dir, "receipts_"+formatBlockForTest(start)+".parquet")
		_, err := os.Stat(path)
		require.True(t, os.IsNotExist(err), "expected receipts_%d.parquet to be removed", start)
	}

	path10 := filepath.Join(dir, "receipts_10.parquet")
	_, err = os.Stat(path10)
	require.NoError(t, err, "receipts_10 must remain because it overlaps the keep window")
}

// TestStoreCrashAndReopen verifies SimulateCrash + reopen leaves the directory
// in a state the new store can read.
func TestStoreCrashAndReopen(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 5,
		TxIndexBackend:   "none",
	})
	require.NoError(t, err)

	// Write some pre-rotation blocks. We do not write the rotation boundary so
	// the active file is unfinished when we simulate a crash.
	for block := uint64(1); block <= 3; block++ {
		require.NoError(t, store.WriteReceipts([]ReceiptInput{makeReceipt(block)}))
	}

	store.SimulateCrash()

	// Reopen — the corrupt active file should be cleaned up by the reader's
	// startup validation, and the WAL should drive recovery.
	store2, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 5,
		TxIndexBackend:   "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store2.Close() })

	// Best-effort: a fresh open should not error. WAL-driven block recovery
	// happens via the higher-level wrapper, so we don't assert receipt content
	// here — that's covered in receipt/parquet_v2/store_test.go.
	require.GreaterOrEqual(t, store2.LatestVersion(), int64(0))
}

// TestCoordinatorSerializesConcurrentWrites verifies that even when callers
// fire concurrent WriteReceipts the coordinator processes them serially and
// no internal mutex is required for correctness.
func TestCoordinatorSerializesConcurrentWrites(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 100,
		TxIndexBackend:   "none",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	const n = 20
	errCh := make(chan error, n)
	for i := 1; i <= n; i++ {
		i := i
		go func() {
			errCh <- store.WriteReceipts([]ReceiptInput{makeReceipt(uint64(i))})
		}()
	}
	for i := 0; i < n; i++ {
		require.NoError(t, <-errCh)
	}

	require.NoError(t, store.Flush())
	// Force rotation to make blocks readable.
	require.NoError(t, store.WriteReceipts([]ReceiptInput{makeReceipt(100)}))

	// Spot-check a few blocks.
	ctx := context.Background()
	for _, b := range []uint64{1, 5, 17} {
		txHash := common.BigToHash(new(big.Int).SetUint64(b))
		result, err := store.GetReceiptByTxHash(ctx, txHash)
		require.NoError(t, err)
		require.NotNil(t, result, "block %d should be readable", b)
		require.Equal(t, b, result.BlockNumber)
	}
}

// dirSize returns the cumulative byte size of files in dir (non-recursive).
func dirSize(t *testing.T, dir string) int64 {
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

func formatBlockForTest(n uint64) string {
	return uintToString(n)
}

func uintToString(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
