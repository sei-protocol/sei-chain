package seiwal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestIteratorRejectsCorruptSealedFile verifies that interior corruption in a sealed file is surfaced as an
// error rather than silently truncating iteration short of the file's name-promised last index. Open no longer
// reads sealed contents, so the iterator's per-record CRC re-read is where such corruption is caught at
// point-of-use (the offline VerifyIntegrity also finds it on demand).
func TestIteratorRejectsCorruptSealedFile(t *testing.T) {
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

	w2 := openWAL(t, cfg) // healthy at open; validation passes
	defer func() { require.NoError(t, w2.Close()) }()

	// Corrupt the last record's CRC only now, after open, so the parser stops short of index 5 on the
	// iterator's re-read of the file from disk.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[len(data)-1] ^= 0xFF
	require.NoError(t, os.WriteFile(path, data, 0o600))

	it, err := w2.Iterator(1, 5)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	var iterErr error
	for {
		ok, err := it.Next()
		if err != nil {
			iterErr = err
			break
		}
		if !ok {
			break
		}
	}
	require.Error(t, iterErr)
	require.Contains(t, iterErr.Error(), "corrupt")
}

// TestIteratorRejectsCorruptMutableSnapshot verifies that interior corruption in the still-unsealed mutable
// file, within the range the iterator's point-in-time snapshot promises, is surfaced as an error rather than
// silently truncating. Those records were complete when the snapshot was taken, so a short read is corruption,
// not a live-write torn tail — and iteration must not depend on whether the file later gets sealed for real.
func TestIteratorRejectsCorruptMutableSnapshot(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir) // large target: records 1..5 stay in the unsealed mutable file, sequence 0

	w := openWAL(t, cfg)
	for index := uint64(1); index <= 5; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush()) // records readable on disk; the file is still unsealed
	defer func() { require.NoError(t, w.Close()) }()
	require.Empty(t, sealedFileNames(t, dir))

	// Corrupt record 5's CRC in the mutable file, within the [1,5] range the snapshot will promise.
	path := filepath.Join(dir, unsealedFileName(0))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[len(data)-1] ^= 0xFF
	require.NoError(t, os.WriteFile(path, data, 0o600))

	it, err := w.Iterator(1, 5)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	var iterErr error
	for {
		ok, err := it.Next()
		if err != nil {
			iterErr = err
			break
		}
		if !ok {
			break
		}
	}
	require.Error(t, iterErr)
	require.Contains(t, iterErr.Error(), "corrupt")
}

// TestIteratorEmptyWALErrors verifies that an empty WAL has no latest index, so any requested end index is
// beyond it: iterator creation fails with ErrIteratorRange rather than returning an empty iterator, and the
// rejection leaves the WAL usable.
func TestIteratorEmptyWALErrors(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	_, err := w.Iterator(0, 0)
	require.ErrorIs(t, err, ErrIteratorRange)

	// The WAL is unharmed by the rejected request: it still accepts appends and iterates.
	appendRecord(t, w, 1)
	require.NoError(t, w.Flush())
	require.Equal(t, []uint64{1}, collectIndices(t, w, 1, 1))
}

// TestIteratorRangeErrors verifies the two invalid-range rejections on a non-empty WAL: an end index below the
// start index, and an end index beyond the latest stored index. Both report ErrIteratorRange and leave the WAL
// usable.
func TestIteratorRangeErrors(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()
	for index := uint64(1); index <= 5; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	_, err := w.Iterator(3, 2)
	require.ErrorIs(t, err, ErrIteratorRange, "end index below start index must be rejected")

	_, err = w.Iterator(1, 6)
	require.ErrorIs(t, err, ErrIteratorRange, "end index beyond the latest stored index must be rejected")

	// Neither rejection bricked the WAL: a valid request still works.
	require.Equal(t, []uint64{1, 2, 3, 4, 5}, collectIndices(t, w, 1, 5))
}

// TestIteratorSnapshotIOFailureDoesNotBrick verifies that a filesystem failure while building the iterator's
// hard-link snapshot is surfaced to the caller without bricking the WAL. Such a failure touches only the
// ephemeral iterator/<serial>/ lease directory, never the WAL data files or the in-memory write state, so the
// WAL must keep accepting appends and, once the fault clears, serving iterators.
func TestIteratorSnapshotIOFailureDoesNotBrick(t *testing.T) {
	dir := t.TempDir()
	w := openWAL(t, testConfig(dir)).(*walImpl)
	defer func() { require.NoError(t, w.Close()) }()

	for index := uint64(1); index <= 5; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	// Occupy the iterator root with a regular file so MkdirAll of iterator/<serial>/ fails with ENOTDIR. The
	// mutable-file flush inside startIterator still succeeds, so this exercises only the non-corrupting snapshot
	// failure, not the fatal flush path.
	require.NoError(t, os.WriteFile(iteratorRoot(dir), []byte("x"), 0o600))

	_, err := w.Iterator(1, 5)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrIteratorRange, "a snapshot I/O failure is not a caller range error")

	// The failed request left the WAL healthy: no fatal cause recorded, and appends still succeed.
	require.NoError(t, w.asyncError())
	appendRecord(t, w, 6)
	require.NoError(t, w.Flush())

	// Once the fault clears, iteration works again and sees every record, including the one appended after the
	// failed request.
	require.NoError(t, os.Remove(iteratorRoot(dir)))
	require.Equal(t, []uint64{1, 2, 3, 4, 5, 6}, collectIndices(t, w, 1, 6))
}

// TestIteratorStopsAtEndIndex verifies the core new behavior: the iterator yields no record beyond the
// requested end index, even though later records exist in the WAL.
func TestIteratorStopsAtEndIndex(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()
	for index := uint64(1); index <= 10; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	require.Equal(t, []uint64{3, 4, 5, 6, 7}, collectIndices(t, w, 3, 7))
}

func TestIteratorFromMiddle(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()
	for index := uint64(1); index <= 5; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	require.Equal(t, []uint64{3, 4, 5}, collectIndices(t, w, 3, 5))
}

func TestIteratorAcrossFiles(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.TargetFileSize = 1 // one record per file
	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for index := uint64(1); index <= 5; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	require.Equal(t, []uint64{2, 3, 4, 5}, collectIndices(t, w, 2, 5))
}

func TestIteratorWithTinyPrefetchBuffer(t *testing.T) {
	// A prefetch buffer smaller than the number of records exercises reader backpressure: the reader must
	// block on a full channel and resume as the consumer drains, without deadlocking or dropping records.
	cfg := testConfig(t.TempDir())
	cfg.IteratorPrefetchSize = 1
	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for index := uint64(1); index <= 20; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	require.Equal(t, []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
		collectIndices(t, w, 1, 20))
}

func TestIteratorCloseBeforeDrainDoesNotLeak(t *testing.T) {
	// Closing an iterator before consuming it must unblock and shut down the reader goroutine cleanly.
	cfg := testConfig(t.TempDir())
	cfg.IteratorPrefetchSize = 1
	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for index := uint64(1); index <= 20; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1, 20)
	require.NoError(t, err)
	// Consume just one record, then close while the reader is still mid-stream (blocked on the full buffer).
	ok, err := it.Next()
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, it.Close())
	require.NoError(t, it.Close()) // idempotent
}

func TestIteratorReaderExitsWhenWALTornDownWhileOrphaned(t *testing.T) {
	// Regression: an iterator whose reader fills the prefetch buffer and is never consumed or Closed — as
	// happens when Iterator() is aborted via ctx.Done() and the constructed iterator is returned to no one —
	// must not leave its reader goroutine blocked on send() forever. Watching the WAL context lets the reader
	// exit when the WAL is torn down, so it cannot become a zombie.
	cfg := testConfig(t.TempDir())
	cfg.IteratorPrefetchSize = 1
	w := openWAL(t, cfg)
	for index := uint64(1); index <= 20; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1, 20)
	require.NoError(t, err)
	iter := it.(*walIterator)

	// Do not consume the iterator: the reader fills the prefetch buffer (size 1) and blocks on send. Tear
	// down the WAL context out from under it, as fail() or Close() would.
	w.(*walImpl).cancel(nil)

	select {
	case <-iter.readerExited:
	case <-time.After(5 * time.Second):
		t.Fatal("reader goroutine did not exit after the WAL context was cancelled")
	}

	// A consumer that races the teardown drains whatever the reader had already buffered, then observes an
	// error rather than a clean EOF: a truncated iteration must never masquerade as fully consumed.
	var termErr error
	for i := 0; i < 25; i++ {
		ok, err := it.Next()
		if err != nil {
			termErr = err
			break
		}
		if !ok {
			break
		}
	}
	require.Error(t, termErr, "truncated iteration must surface an error, not a clean EOF")
}

func TestIteratorYieldsRecordContents(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	require.NoError(t, w.Append(1, []byte("hello world")))
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1, 1)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	ok, err := it.Next()
	require.NoError(t, err)
	require.True(t, ok)
	index, data := it.Entry()
	require.Equal(t, uint64(1), index)
	require.Equal(t, []byte("hello world"), data)

	ok, err = it.Next()
	require.NoError(t, err)
	require.False(t, ok)
}

// TestConcurrentIterationDuringRotation hammers the writer with rotate-on-every-record churn while several
// iterators read concurrently. Each iterator seals the mutable file and snapshots its file set at creation on
// the writer goroutine, so every file it reads is sealed and immutable and can never be renamed or rewritten
// out from under an in-flight read; every iteration must be error-free and gap-free.
func TestConcurrentIterationDuringRotation(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.TargetFileSize = 1 // rotate (rename) after every record, maximizing the seal/rename churn
	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()

	const totalRecords = 300
	const readers = 4
	const iterationsPerReader = 40

	var wg sync.WaitGroup

	writeErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for index := uint64(1); index <= totalRecords; index++ {
			if err := w.Append(index, recordPayload(index)); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- nil
	}()

	readerErr := make(chan error, readers)
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterationsPerReader; i++ {
				if err := drainContiguousFrom(w, 1); err != nil {
					readerErr <- err
					return
				}
			}
			readerErr <- nil
		}()
	}

	wg.Wait()
	require.NoError(t, <-writeErr)
	for r := 0; r < readers; r++ {
		require.NoError(t, <-readerErr)
	}
}

// drainContiguousFrom fully consumes an iterator over [start, currentLast], verifying the yielded indices
// form a gap-free, strictly-increasing run beginning at start (an empty run is allowed: the writer may not
// have produced start yet). The end is read from the WAL's current bounds so a concurrent writer's latest
// index does not need to be known in advance. Returns the first error encountered.
func drainContiguousFrom(w WAL[[]byte], start uint64) error {
	ok, _, last, err := w.Bounds()
	if err != nil {
		return fmt.Errorf("bounds: %w", err)
	}
	if !ok || last < start {
		return nil // nothing at or above start yet
	}
	it, err := w.Iterator(start, last)
	if err != nil {
		return fmt.Errorf("create iterator: %w", err)
	}
	prev := start - 1
	for {
		ok, err := it.Next()
		if err != nil {
			_ = it.Close()
			return fmt.Errorf("next: %w", err)
		}
		if !ok {
			break
		}
		index, _ := it.Entry()
		if index != prev+1 {
			_ = it.Close()
			return fmt.Errorf(
				"non-contiguous iteration: got index %d after %d (start %d)", index, prev, start)
		}
		prev = index
	}
	return it.Close()
}

// TestIteratorDoesNotSeePostConstructionRecords pins down the snapshot contract: an iterator yields only
// records that existed when it was created, never records appended afterward. Because Iterator() seals the
// mutable file at creation, a record appended after lands in a fresh file outside the snapshot and must not
// appear.
func TestIteratorDoesNotSeePostConstructionRecords(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir())) // large target: records begin in one mutable file
	defer func() { require.NoError(t, w.Close()) }()

	for index := uint64(1); index <= 3; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1, 3)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	// Appended after the iterator exists, before draining: must not be observed.
	require.NoError(t, w.Append(4, recordPayload(4)))
	require.NoError(t, w.Flush())

	var got []uint64
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		index, _ := it.Entry()
		got = append(got, index)
	}
	require.Equal(t, []uint64{1, 2, 3}, got, "post-construction record 4 must not be iterated")
}

// TestIteratorSurfacesFatalCause verifies that when the WAL bricks while an iterator is live, Next surfaces the
// recorded fatal cause rather than a bare context.Canceled — so a disk failure during replay is not buried
// under a generic "context canceled" message, matching the asyncError-first pattern used elsewhere in walImpl.
func TestIteratorSurfacesFatalCause(t *testing.T) {
	cfg := testConfig(t.TempDir())
	w := openWAL(t, cfg).(*walImpl)
	defer func() { _ = w.Close() }()

	// More records than the reader can prefetch, so it is still producing (not at a clean EOF) when the WAL
	// bricks, guaranteeing Next reaches the shutdown branch rather than a normal end-of-iteration.
	const n = 100
	for i := uint64(1); i <= n; i++ {
		appendRecord(t, w, i)
	}
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1, n)
	require.NoError(t, err)
	defer func() { _ = it.Close() }()

	// Brick from the writer goroutine: close the mutable fd out from under it, then a flush fails and calls fail().
	require.NoError(t, w.mutableFile.file.Close())
	require.NoError(t, w.Append(uint64(n+1), recordPayload(uint64(n+1))))
	require.Error(t, w.Flush())

	select {
	case <-w.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("WAL did not brick after flush failure")
	}
	cause := w.asyncError()
	require.Error(t, cause)

	// Drain whatever the reader had buffered; the first error must carry the fatal cause, not context.Canceled.
	var got error
	for {
		ok, nextErr := it.Next()
		if nextErr != nil {
			got = nextErr
			break
		}
		if !ok {
			break
		}
	}
	require.Error(t, got)
	require.ErrorIs(t, got, cause)
	require.NotErrorIs(t, got, context.Canceled)
}
