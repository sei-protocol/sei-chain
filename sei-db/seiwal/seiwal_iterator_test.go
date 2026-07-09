package seiwal

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestIteratorRejectsCorruptSealedFile verifies that interior corruption in a sealed file is surfaced as an
// error rather than silently truncating iteration short of the file's name-promised last index. A sealed file
// is durable and complete, so any shortfall between its content and its name is corruption, not a torn tail.
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
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[len(data)-1] ^= 0xFF // corrupt the last record's CRC so the parser stops short of index 5
	require.NoError(t, os.WriteFile(path, data, 0o600))

	w2 := openWAL(t, cfg)
	defer func() { require.NoError(t, w2.Close()) }()

	it, err := w2.Iterator(1)
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

func TestIteratorEmpty(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	it, err := w.Iterator(0)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	ok, err := it.Next()
	require.NoError(t, err)
	require.False(t, ok)
}

func TestIteratorFromMiddle(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()
	for index := uint64(1); index <= 5; index++ {
		appendRecord(t, w, index)
	}
	require.NoError(t, w.Flush())

	require.Equal(t, []uint64{3, 4, 5}, collectIndices(t, w, 3))
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

	require.Equal(t, []uint64{2, 3, 4, 5}, collectIndices(t, w, 2))
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
		collectIndices(t, w, 1))
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

	it, err := w.Iterator(1)
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

	it, err := w.Iterator(1)
	require.NoError(t, err)
	iter := it.(*walIterator)

	// Do not consume the iterator: the reader fills the prefetch buffer (size 1) and blocks on send. Tear
	// down the WAL context out from under it, as fail() or Close() would.
	w.(*walImpl).cancel()

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

	it, err := w.Iterator(1)
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

// drainContiguousFrom fully consumes an iterator anchored at start, verifying the yielded indices form a
// gap-free, strictly-increasing run beginning at start (an empty run is allowed: the writer may not have
// produced start yet). Returns the first error encountered.
func drainContiguousFrom(w WAL[[]byte], start uint64) error {
	it, err := w.Iterator(start)
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
			return fmt.Errorf("non-contiguous iteration: got index %d after %d (start %d)", index, prev, start)
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

	it, err := w.Iterator(1)
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
