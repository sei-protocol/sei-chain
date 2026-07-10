package seiwal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestQueueDepthSamplerRunsAndStops exercises the queue-depth sampler goroutine on a tiny interval: it must
// sample the writer channel concurrently with appends (validated by the race detector) and shut down cleanly
// on Close.
func TestQueueDepthSamplerRunsAndStops(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.MetricsSampleInterval = time.Millisecond
	w := openWAL(t, cfg)
	for index := uint64(1); index <= 300; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())
}

func testConfig(dir string) *Config {
	return DefaultConfig(dir, "test")
}

func openWAL(t *testing.T, cfg *Config) WAL[[]byte] {
	t.Helper()
	w, err := NewWAL(cfg)
	require.NoError(t, err)
	return w
}

// recordPayload returns a deterministic payload for a record index.
func recordPayload(index uint64) []byte {
	return []byte(fmt.Sprintf("payload-%d", index))
}

// appendRecord appends a record with recordPayload(index) at the given index.
func appendRecord(t *testing.T, w WAL[[]byte], index uint64) {
	t.Helper()
	require.NoError(t, w.Append(index, recordPayload(index)))
}

// collectIndices iterates from start and returns the index of each record, verifying that indices are
// strictly increasing and never below start.
func collectIndices(t *testing.T, w WAL[[]byte], start uint64) []uint64 {
	t.Helper()
	it, err := w.Iterator(start)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	var indices []uint64
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		index, _ := it.Entry()
		require.GreaterOrEqual(t, index, start)
		if len(indices) > 0 {
			require.Greater(t, index, indices[len(indices)-1])
		}
		indices = append(indices, index)
	}
	return indices
}

func countSealedFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	count := 0
	for _, entry := range entries {
		if parsed, ok := parseFileName(entry.Name()); ok && parsed.sealed {
			count++
		}
	}
	return count
}

// sealedFileNames returns the names of all sealed WAL files in dir, sorted for stable assertions.
func sealedFileNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		if parsed, ok := parseFileName(entry.Name()); ok && parsed.sealed {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func TestAppendFlushReopenBounds(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	w := openWAL(t, cfg)
	for index := uint64(1); index <= 5; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	ok, first, last, err := w.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(5), last)
	require.NoError(t, w.Close())

	w2 := openWAL(t, cfg)
	defer func() { require.NoError(t, w2.Close()) }()

	ok, first, last, err = w2.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(5), last)

	require.Equal(t, []uint64{1, 2, 3, 4, 5}, collectIndices(t, w2, 1))
}

func TestAppendOrdering(t *testing.T) {
	t.Run("index must strictly increase", func(t *testing.T) {
		w := openWAL(t, testConfig(t.TempDir()))
		defer func() { require.NoError(t, w.Close()) }()
		require.NoError(t, w.Append(5, recordPayload(5)))
		require.Error(t, w.Append(4, recordPayload(4)))
		require.Error(t, w.Append(5, recordPayload(5)))
	})

	t.Run("non-contiguous indices are allowed", func(t *testing.T) {
		w := openWAL(t, testConfig(t.TempDir()))
		defer func() { require.NoError(t, w.Close()) }()
		require.NoError(t, w.Append(1, recordPayload(1)))
		require.NoError(t, w.Append(3, recordPayload(3)))
		require.NoError(t, w.Append(100, recordPayload(100)))
		require.NoError(t, w.Flush())
		require.Equal(t, []uint64{1, 3, 100}, collectIndices(t, w, 0))
	})

	t.Run("empty payload is allowed", func(t *testing.T) {
		w := openWAL(t, testConfig(t.TempDir()))
		defer func() { require.NoError(t, w.Close()) }()
		require.NoError(t, w.Append(1, nil))
		require.NoError(t, w.Append(2, []byte{}))
		require.NoError(t, w.Flush())

		it, err := w.Iterator(1)
		require.NoError(t, err)
		defer func() { require.NoError(t, it.Close()) }()
		ok, err := it.Next()
		require.NoError(t, err)
		require.True(t, ok)
		index, data := it.Entry()
		require.Equal(t, uint64(1), index)
		require.Empty(t, data)
	})
}

// TestWriterRejectsOutOfOrderRecord exercises the writer-goroutine backstop directly. The caller-side gate
// in Append can be bypassed by concurrent misuse (the reordering race is non-deterministic), so appendRecord
// re-asserts strict index increase itself. Driving appendRecord on a standalone walImpl (no running writer
// loop) verifies that a non-increasing index is rejected rather than written with inverted bounds.
func TestWriterRejectsOutOfOrderRecord(t *testing.T) {
	dir := t.TempDir()
	mf, err := newWalFile(dir, 0)
	require.NoError(t, err)
	w := &walImpl{
		config:      testConfig(dir),
		metricAttrs: walNameAttr("test"),
		ctx:         context.Background(),
		mutableFile: mf,
	}
	defer func() { _, _ = w.mutableFile.seal() }()

	write := func(index uint64) error {
		return w.appendRecord(dataToBeWritten{record: frameRecord(index, recordPayload(index)), index: index})
	}

	require.NoError(t, write(5))
	require.Error(t, write(4)) // lower than last written
	require.Error(t, write(5)) // equal to last written
	require.NoError(t, write(6))
	require.Equal(t, uint64(6), w.lastWrittenIndex)
}

// TestFailReleasesMutableFile verifies that a fatal error releases the mutable file's handle (rather than
// leaking the fd until process exit) and that the release is idempotent.
func TestFailReleasesMutableFile(t *testing.T) {
	dir := t.TempDir()
	mf, err := newWalFile(dir, 0)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	w := &walImpl{
		config:      testConfig(dir),
		metricAttrs: walNameAttr("test"),
		ctx:         ctx,
		cancel:      cancel,
		mutableFile: mf,
	}
	require.NoError(t, w.appendRecord(dataToBeWritten{record: frameRecord(1, recordPayload(1)), index: 1}))

	w.fail(fmt.Errorf("boom"))

	require.Nil(t, w.mutableFile.file)        // fd released
	require.Error(t, w.asyncError())          // failure recorded
	require.NoError(t, w.mutableFile.close()) // idempotent
}

// TestFlushIOFailureBricksWAL verifies that an IO error during Flush is fatal: the failure is surfaced to the
// flushing caller, the WAL then refuses all further work, and Close reports the original error — matching how
// every other writer IO error is handled, so a broken durability guarantee is never silently tolerated.
func TestFlushIOFailureBricksWAL(t *testing.T) {
	dir := t.TempDir()
	w := openWAL(t, testConfig(dir))

	impl, ok := w.(*walImpl)
	require.True(t, ok)

	// Force the next flush to fail by closing the mutable file's descriptor out from under the writer. The
	// writer is idle (blocked awaiting a message) and never reassigns the handle, and appending only buffers
	// bytes, so this affects nothing until the flush attempts to write/fsync the closed descriptor.
	require.NoError(t, impl.mutableFile.file.Close())

	require.NoError(t, w.Append(1, recordPayload(1)))
	require.Error(t, w.Flush(), "flush must surface the IO failure")

	// Bricking cancels the context; wait for it so the "refuses further work" assertions are deterministic
	// (Flush may return the moment the error is sent, a hair before fail() finishes tearing down).
	select {
	case <-impl.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("WAL did not brick after flush failure")
	}

	require.Error(t, w.Append(2, recordPayload(2)), "appends must fail on a bricked WAL")
	require.Error(t, w.Flush(), "flush must fail on a bricked WAL")
	_, _, _, err := w.Bounds()
	require.Error(t, err, "bounds must fail on a bricked WAL")

	require.Error(t, w.Close(), "Close must surface the fatal flush error")
	require.Error(t, impl.asyncError())
}

// TestIteratorRotateFailureBricksWAL verifies that when the rotation performed during iterator creation fails
// at opening the fresh mutable file (after the current file was already sealed), the WAL bricks itself rather
// than limping on with an inconsistent mutable file and later staging a phantom sealed entry.
func TestIteratorRotateFailureBricksWAL(t *testing.T) {
	dir := t.TempDir()
	w := openWAL(t, testConfig(dir))

	impl, ok := w.(*walImpl)
	require.True(t, ok)

	require.NoError(t, w.Append(1, recordPayload(1)))
	require.NoError(t, w.Flush())

	// Make the rotation's newWalFile step fail while its seal step still succeeds: occupy the exact path the
	// next mutable file wants (fileSeq 1 -> "1.wal.u") with a directory, so os.Create there fails with EISDIR.
	// The seal renames the current file to "0-1-1.wal", unaffected by this blocker.
	blocker := filepath.Join(dir, unsealedFileName(1))
	require.NoError(t, os.Mkdir(blocker, 0o755))

	_, err := w.Iterator(1)
	require.Error(t, err, "iterator creation must surface the rotation failure")

	select {
	case <-impl.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("WAL did not brick after rotation failure during iterator creation")
	}

	require.Error(t, w.Append(2, recordPayload(2)), "appends must fail on a bricked WAL")
	require.Error(t, w.Close(), "Close must surface the fatal error")
	require.Error(t, impl.asyncError())
}

func TestOrphanFileRecovery(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	// Fabricate an orphaned unsealed file: records 1 and 2 intact, a torn record 3, left unsealed as if the
	// process crashed before it could seal.
	f, err := newWalFile(dir, 0)
	require.NoError(t, err)
	writeRecordTo(t, f, 1, recordPayload(1))
	writeRecordTo(t, f, 2, recordPayload(2))
	frame := frameRecord(3, recordPayload(3))
	require.NoError(t, f.flush(false))
	_, err = f.writer.Write(frame[:len(frame)-3]) // torn record 3
	require.NoError(t, err)
	require.NoError(t, f.flush(true))
	require.NoError(t, f.file.Close())

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()

	ok, first, last, err := w.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(2), last)
	require.Equal(t, []uint64{1, 2}, collectIndices(t, w, 1))
}

func TestRotationProducesContiguousSealedFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1 // rotate after every record

	w := openWAL(t, cfg)
	for index := uint64(1); index <= 6; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	ok, first, last, err := w.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(6), last)
	require.Equal(t, []uint64{1, 2, 3, 4, 5, 6}, collectIndices(t, w, 1))
	require.NoError(t, w.Close())

	// Every record should have produced its own sealed file with a clean [k,k] range.
	var sealed []parsedFileName
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if parsed, okName := parseFileName(entry.Name()); okName && parsed.sealed {
			sealed = append(sealed, parsed)
			require.Equal(t, parsed.firstIndex, parsed.lastIndex)
		}
	}
	require.Len(t, sealed, 6)
}

func TestRecordNeverSplitAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 128 // tiny, so a single record dwarfs the rotation threshold

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()

	// Two records, each far larger than TargetFileSize.
	big1 := make([]byte, 4096)
	big2 := make([]byte, 4096)
	for i := range big1 {
		big1[i] = byte(i)
		big2[i] = byte(i + 1)
	}
	require.NoError(t, w.Append(1, big1))
	require.NoError(t, w.Append(2, big2))
	require.NoError(t, w.Flush())

	// Each oversized record rotated into its own file, intact — never split across files.
	require.Equal(t, 2, countSealedFiles(t, dir))

	it, err := w.Iterator(1)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	ok, err := it.Next()
	require.NoError(t, err)
	require.True(t, ok)
	index, data := it.Entry()
	require.Equal(t, uint64(1), index)
	require.Equal(t, big1, data)

	ok, err = it.Next()
	require.NoError(t, err)
	require.True(t, ok)
	index, data = it.Entry()
	require.Equal(t, uint64(2), index)
	require.Equal(t, big2, data)

	ok, err = it.Next()
	require.NoError(t, err)
	require.False(t, ok)
}

func TestPruneDropsWholeFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1 // one record per file, so pruning can drop whole files

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for index := uint64(1); index <= 10; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	require.NoError(t, w.Prune(5))

	ok, first, last, err := w.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(5), first)
	require.Equal(t, uint64(10), last)
	require.Equal(t, []uint64{5, 6, 7, 8, 9, 10}, collectIndices(t, w, 0))
}

func TestPrunePastAllRecordsEmptiesRange(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1 // one record per file so every record sits in a prunable sealed file

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for index := uint64(1); index <= 5; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	require.NoError(t, w.Prune(100))

	ok, _, _, err := w.Bounds()
	require.NoError(t, err)
	require.False(t, ok)
}

func TestActiveIteratorBlocksPruningOfNeededFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1 // one record per sealed file, so pruning works file-by-file

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for index := uint64(1); index <= 10; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	// Hold an iterator anchored at index 1 (the oldest). Its read lease must keep index 1's file alive.
	it, err := w.Iterator(1)
	require.NoError(t, err)

	require.NoError(t, w.Prune(5))
	ok, first, last, err := w.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first, "index 1 must survive pruning while a live iterator pins it")
	require.Equal(t, uint64(10), last)

	// The iterator still sees the full, intact sequence.
	require.Equal(t, []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, collectIndices(t, w, 1))

	// Releasing the lease lets the same prune make progress.
	require.NoError(t, it.Close())
	require.NoError(t, w.Prune(5))
	ok, first, _, err = w.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(5), first)
}

func TestIteratorAnchoredAboveKeepPointDoesNotBlockPruning(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for index := uint64(1); index <= 10; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	// An iterator anchored at index 8 does not need records below 5, so pruning to 5 proceeds.
	it, err := w.Iterator(8)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	require.NoError(t, w.Prune(5))
	ok, first, _, err := w.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(5), first)
}

// TestIteratorInGapBlocksPruningAcrossGap covers the index gap case: indices may jump, so an iterator's read
// lease can land in a gap between stored files. Pruning must still protect every file the iterator will read
// (those reaching the lease index or higher), even though no file's range contains the lease index itself.
// The directory is inspected directly rather than relying on iterator output, since the reader goroutine may
// have buffered the files into memory before an unsafe delete.
func TestIteratorInGapBlocksPruningAcrossGap(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1 // one record per sealed file

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	// Indices 1,2,3 then a legal jump to 10,11,12. The lease index 5 falls in the gap (3, 10).
	for _, index := range []uint64{1, 2, 3, 10, 11, 12} {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	it, err := w.Iterator(5)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	// Prune(12) would remove every file with last index < 12, but the live lease at 5 must keep the files for
	// indices 10 and 11 (both >= 5). Only the files entirely below the lease (indices 1,2,3) may be dropped.
	require.NoError(t, w.Prune(12))
	_, _, _, err = w.Bounds() // synchronous round-trip forces the async prune to complete
	require.NoError(t, err)

	names := sealedFileNames(t, dir)
	require.Contains(t, names, sealedFileName(3, 10, 10), "file for index 10 must survive while iterator(5) is live")
	require.Contains(t, names, sealedFileName(4, 11, 11), "file for index 11 must survive while iterator(5) is live")
	require.NotContains(t, names, sealedFileName(0, 1, 1), "file for index 1 is below the lease and should be pruned")

	require.Equal(t, []uint64{10, 11, 12}, collectIndices(t, w, 5))
}

// TestIteratorLeaseInsideFileRangeBlocksPruning checks the boundary where the lease index sits within the kept
// window: an iterator anchored at 5 must keep indices 5..10 even as pruning is asked to drop through a higher
// point, because those files reach the lease index or higher.
func TestIteratorLeaseInsideFileRangeBlocksPruning(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1 // one record per sealed file

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for index := uint64(1); index <= 10; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	it, err := w.Iterator(5)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	require.NoError(t, w.Prune(8))
	ok, first, last, err := w.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(5), first, "lease at 5 must keep records from 5 onward")
	require.Equal(t, uint64(10), last)
	require.Equal(t, []uint64{5, 6, 7, 8, 9, 10}, collectIndices(t, w, 5))
}

func TestScanRejectsGapInSealedFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1 // one record per sealed file

	w := openWAL(t, cfg)
	for index := uint64(1); index <= 4; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Close())

	// Delete a middle sealed file to punch a gap in the sequence, simulating corruption.
	var sealed []parsedFileName
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if p, ok := parseFileName(entry.Name()); ok && p.sealed {
			sealed = append(sealed, p)
		}
	}
	require.GreaterOrEqual(t, len(sealed), 3)
	sort.Slice(sealed, func(i int, j int) bool { return sealed[i].fileSeq < sealed[j].fileSeq })
	victim := sealed[len(sealed)/2]
	require.NoError(t, os.Remove(filepath.Join(dir, sealedFileName(victim.fileSeq, victim.firstIndex, victim.lastIndex))))

	_, err = NewWAL(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not contiguous")
}

// TestNewWALRejectsMidStreamCorruptSealedFile verifies that a checksum mismatch in a non-final record of a
// sealed file is surfaced as a hard error at open. Corrupted durable data demands human intervention, so the
// WAL must refuse to open rather than silently serving a view truncated at the corruption point.
func TestNewWALRejectsMidStreamCorruptSealedFile(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	w := openWAL(t, cfg)
	for index := uint64(1); index <= 5; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Close()) // seals records 1..5 into a single file

	names := sealedFileNames(t, dir)
	require.Len(t, names, 1)
	path := filepath.Join(dir, names[0])
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	// Flip a byte in the first record's payload so the fault is mid-stream, not a torn trailing record. The
	// first record's payload begins just past the header and its two single-byte uvarint prefixes (index 1,
	// length 9), so walHeaderSize+2 lands inside the payload.
	data[walHeaderSize+2] ^= 0xFF
	require.NoError(t, os.WriteFile(path, data, 0o600))

	_, err = NewWAL(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt")
}

// TestNewWALRejectsTruncatedSealedFile verifies that a sealed file truncated at a clean record boundary — all
// remaining records checksum correctly, but the content stops short of the last index its name promises — is
// rejected at open. This is the case parse-strictness alone cannot catch (no torn record remains); the
// content/name range check must.
func TestNewWALRejectsTruncatedSealedFile(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	w := openWAL(t, cfg)
	for index := uint64(1); index <= 5; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Close()) // seals records 1..5 into a single file named 0-1-5

	names := sealedFileNames(t, dir)
	require.Len(t, names, 1)
	path := filepath.Join(dir, names[0])

	// Truncate at the boundary just past the 4th record, leaving indices 1..4 intact while the name still
	// promises [1, 5].
	contents, err := readWalFile(path)
	require.NoError(t, err)
	require.Len(t, contents.records, 5)
	require.NoError(t, os.Truncate(path, contents.records[3].end))

	_, err = NewWAL(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt")
}

// TestNewWALRejectsSealedFileBadMagic verifies that a sealed file with a clobbered header (invalid magic
// prefix) is rejected at open rather than treated as empty.
func TestNewWALRejectsSealedFileBadMagic(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	w := openWAL(t, cfg)
	for index := uint64(1); index <= 3; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Close())

	names := sealedFileNames(t, dir)
	require.Len(t, names, 1)
	path := filepath.Join(dir, names[0])
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[0] ^= 0xFF // clobber the magic prefix
	require.NoError(t, os.WriteFile(path, data, 0o600))

	_, err = NewWAL(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt")
}

func TestBoundsEmpty(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	ok, _, _, err := w.Bounds()
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRollbackConstructor(t *testing.T) {
	t.Run("drops whole files beyond the rollback point", func(t *testing.T) {
		dir := t.TempDir()
		cfg := testConfig(dir)
		cfg.TargetFileSize = 1 // one record per file

		w := openWAL(t, cfg)
		for index := uint64(1); index <= 6; index++ {
			appendRecord(t, w, index)
		}
		require.NoError(t, w.Close())

		w2, err := NewWALWithRollback(cfg, 3)
		require.NoError(t, err)
		defer func() { require.NoError(t, w2.Close()) }()

		ok, first, last, err := w2.Bounds()
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, uint64(1), first)
		require.Equal(t, uint64(3), last)
		require.Equal(t, []uint64{1, 2, 3}, collectIndices(t, w2, 1))
	})

	t.Run("truncates within a file at the rollback point", func(t *testing.T) {
		dir := t.TempDir()
		cfg := testConfig(dir) // large target: all records land in one file

		w := openWAL(t, cfg)
		for index := uint64(1); index <= 6; index++ {
			appendRecord(t, w, index)
		}
		require.NoError(t, w.Close())

		w2, err := NewWALWithRollback(cfg, 3)
		require.NoError(t, err)
		defer func() { require.NoError(t, w2.Close()) }()

		ok, first, last, err := w2.Bounds()
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, uint64(1), first)
		require.Equal(t, uint64(3), last)
		require.Equal(t, []uint64{1, 2, 3}, collectIndices(t, w2, 1))

		// Appending continues cleanly after the rollback point.
		appendRecord(t, w2, 4)
		require.NoError(t, w2.Flush())
		_, _, last, err = w2.Bounds()
		require.NoError(t, err)
		require.Equal(t, uint64(4), last)
	})

	// After a rollback, a subsequent *normal* open (not another rollback) must observe exactly the rolled-back
	// range. This is the path that would expose a name/content mismatch left by a non-crash-safe rollback:
	// Bounds is name-derived while iteration is content-bound, so the two agree only if the truncation and
	// rename were applied consistently. Exercises both rollback shapes: whole-file removal and in-file
	// truncation of the straddling file.
	t.Run("rolled-back state is consistent under a normal reopen", func(t *testing.T) {
		for _, tc := range []struct {
			name       string
			targetSize uint
		}{
			{"whole-file removal", 1},                // one record per file: rollback removes whole trailing files
			{"in-file truncation", 64 * 1024 * 1024}, // all records in one file: rollback truncates it in place
		} {
			t.Run(tc.name, func(t *testing.T) {
				dir := t.TempDir()
				cfg := testConfig(dir)
				cfg.TargetFileSize = tc.targetSize

				w := openWAL(t, cfg)
				for index := uint64(1); index <= 6; index++ {
					appendRecord(t, w, index)
				}
				require.NoError(t, w.Close())

				w2, err := NewWALWithRollback(cfg, 3)
				require.NoError(t, err)
				require.NoError(t, w2.Close())

				// Reopen normally; the rollback must have durably and consistently reduced the range to [1,3].
				w3 := openWAL(t, cfg)
				defer func() { require.NoError(t, w3.Close()) }()

				ok, first, last, err := w3.Bounds()
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, uint64(1), first)
				require.Equal(t, uint64(3), last)
				require.Equal(t, []uint64{1, 2, 3}, collectIndices(t, w3, 1))
			})
		}
	})
}

// recordPrefixBytes reads the sealed file at path and returns the raw bytes of the prefix ending just past the
// record for lastKeep — i.e. the exact content rollbackStraddlingFile's AtomicWrite would install for a
// rollback to lastKeep. It is the test's stand-in for "the truncated copy the rollback would produce".
func recordPrefixBytes(t *testing.T, path string, lastKeep uint64) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	contents, err := readWalFile(path)
	require.NoError(t, err)
	var truncateTo int64
	found := false
	for _, r := range contents.records {
		if r.index == lastKeep {
			truncateTo = r.end
			found = true
			break
		}
	}
	require.True(t, found, "index %d has no record boundary in %s", lastKeep, path)
	return data[:truncateTo]
}

// TestRollbackCrashAfterSwapReconciledOnReopen simulates a crash in rollbackStraddlingFile after the reduced
// file was durably written (AtomicWrite) but before the old, larger-named file was removed. That leaves two
// sealed files sharing a sequence. A subsequent open must reconcile them — keeping the reduced file — so the
// name-derived Bounds and the content-derived iterator agree on the rolled-back range.
func TestRollbackCrashAfterSwapReconciledOnReopen(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir) // large target: all six records land in one file, sequence 0

	w := openWAL(t, cfg)
	for index := uint64(1); index <= 6; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Close())

	oldName := sealedFileName(0, 1, 6)
	require.Equal(t, []string{oldName}, sealedFileNames(t, dir))

	// Reproduce the crash state: the reduced file [1,3] exists next to the untouched original [1,6].
	reducedName := sealedFileName(0, 1, 3)
	prefix := recordPrefixBytes(t, filepath.Join(dir, oldName), 3)
	require.NoError(t, os.WriteFile(filepath.Join(dir, reducedName), prefix, 0o600))
	require.Equal(t, []string{reducedName, oldName}, sealedFileNames(t, dir))

	// A plain reopen must reconcile the duplicate sequence down to the rolled-back file.
	w2 := openWAL(t, cfg)
	defer func() { require.NoError(t, w2.Close()) }()

	require.Equal(t, []string{reducedName}, sealedFileNames(t, dir))
	ok, first, last, err := w2.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(3), last)
	require.Equal(t, []uint64{1, 2, 3}, collectIndices(t, w2, 1))
}

// TestRollbackCrashDuringSwapWindowRecovers simulates a crash mid-rollback in the earlier window: the
// AtomicWrite's swap file was created but not yet renamed into place, so only a leftover ".swap" exists beside
// the still-intact original. A reopen must drop the swap and leave the original range intact (the rollback
// simply did not take effect), and a subsequent rollback must then complete cleanly and durably.
func TestRollbackCrashDuringSwapWindowRecovers(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir) // large target: all six records in one file, sequence 0

	w := openWAL(t, cfg)
	for index := uint64(1); index <= 6; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Close())

	oldName := sealedFileName(0, 1, 6)

	// Reproduce the crash state: an unfinished AtomicWrite left a swap file for the reduced name, alongside
	// the untouched original. util.AtomicWrite names its temp "<destination>.swap".
	prefix := recordPrefixBytes(t, filepath.Join(dir, oldName), 3)
	swapName := sealedFileName(0, 1, 3) + ".swap"
	require.NoError(t, os.WriteFile(filepath.Join(dir, swapName), prefix, 0o600))

	// A plain reopen drops the swap; the original range survives (rollback did not take effect).
	w2 := openWAL(t, cfg)
	require.Equal(t, []string{oldName}, sealedFileNames(t, dir))
	_, err := os.Stat(filepath.Join(dir, swapName))
	require.True(t, os.IsNotExist(err), "leftover swap file should have been removed")
	ok, first, last, err := w2.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(6), last)
	require.NoError(t, w2.Close())

	// The subsequent rollback completes cleanly, and a normal reopen sees the consistent rolled-back range.
	w3, err := NewWALWithRollback(cfg, 3)
	require.NoError(t, err)
	require.NoError(t, w3.Close())

	w4 := openWAL(t, cfg)
	defer func() { require.NoError(t, w4.Close()) }()
	require.Equal(t, []string{sealedFileName(0, 1, 3)}, sealedFileNames(t, dir))
	ok, first, last, err = w4.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(3), last)
	require.Equal(t, []uint64{1, 2, 3}, collectIndices(t, w4, 1))
}
