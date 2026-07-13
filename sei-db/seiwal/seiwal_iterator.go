package seiwal

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var _ Iterator[[]byte] = (*walIterator)(nil)

// A record produced by the reader goroutine, or a terminal error.
type iteratorResult struct {
	index   uint64
	payload []byte
	err     error
}

// iteratorFile is one entry in an iterator's file snapshot, captured on the writer goroutine when the iterator
// is created (see startIterator). name is the file's basename inside the iterator's private hard-link
// directory. A sealed entry is immutable and verified against its [firstIndex, lastIndex] range; a non-sealed
// entry is the hard-linked mutable file, which may still be growing, so it is parsed torn-tolerantly and
// bounded by the iterator's maxIndex.
type iteratorFile struct {
	fileSeq    uint64
	name       string
	firstIndex uint64
	lastIndex  uint64
	sealed     bool
}

// walIterator iterates the WAL a record at a time, in ascending index order. A dedicated reader goroutine
// reads WAL files from disk and pushes each record onto a buffered channel; Next simply dequeues. Decoupling
// disk reads from the consumer keeps the reader busy while the consumer works, which matters for startup
// replay speed. The reader loads one file at a time, so its memory use is bounded by a single WAL file plus
// the prefetch buffer.
//
// The set of files to read is snapshotted once at creation as hard links in a private directory (dir); the
// reader walks that list in O(n) without re-scanning. The links keep their inodes alive against concurrent
// rotation and pruning, so the reader always finds its files; Close removes the directory. Records above
// maxIndex — the highest index stored at creation — are refused, giving a consistent point-in-time view even
// though the hard-linked mutable file may keep growing under the reader.
type walIterator struct {
	// The WAL this iterator reads from.
	wal *walImpl

	// The lowest index the consumer asked for; records below it are skipped.
	start uint64

	// The highest index this iterator yields; records above it (appended after creation, or written to the
	// hard-linked mutable file after the snapshot) are refused, fixing the point-in-time view.
	maxIndex uint64

	// The iterator's private directory of hard-link snapshots (iterator/<serial>/), removed by Close. Empty
	// when the iterator has no files to read, in which case Close removes nothing.
	dir string

	// Records produced by the reader goroutine. Closed by the reader on clean EOF.
	results chan iteratorResult

	// Closed by Close to tell the reader goroutine to stop early.
	stop chan struct{}

	// Closed by the reader goroutine when it exits, so Close can wait for it.
	readerExited chan struct{}

	// Ensures the shutdown sequence runs at most once.
	closeOnce sync.Once

	// The index and payload returned by Entry, set by the most recent successful Next. Owned by the single
	// consumer goroutine (see the Iterator concurrency contract); never touched by Close or the reader.
	resultIndex   uint64
	resultPayload []byte

	// Set once iteration is complete. Owned by the single consumer goroutine (see the Iterator concurrency
	// contract): Next and Close both set it, which is why they must not run concurrently.
	done bool

	// The following fields are owned exclusively by the reader goroutine.

	// The point-in-time snapshot of files to read, in ascending index order. Set once at construction.
	files []iteratorFile
	// The position into files of the next file to load.
	filePos int
	// The records loaded from the current file, filtered to indices at or beyond start.
	buffer []walRecord
	// The position within buffer; -1 before the first record is read.
	pos int
}

// newWalIterator creates an iterator over wal starting at startIndex and launches its reader goroutine.
// maxIndex is the highest index it will yield. files is the snapshot of hard-linked files to read (captured on
// the writer goroutine), living under dir; Close removes dir (empty dir means nothing to read and nothing to
// remove). prefetch is the number of records the reader may buffer ahead of the consumer.
func newWalIterator(
	wal *walImpl,
	startIndex uint64,
	maxIndex uint64,
	dir string,
	files []iteratorFile,
	prefetch uint,
) *walIterator {
	it := &walIterator{
		wal:          wal,
		start:        startIndex,
		maxIndex:     maxIndex,
		dir:          dir,
		results:      make(chan iteratorResult, prefetch),
		stop:         make(chan struct{}),
		readerExited: make(chan struct{}),
		files:        files,
		pos:          -1,
	}
	go it.read()
	return it
}

func (it *walIterator) Next() (bool, error) {
	if it.done {
		return false, nil
	}
	// Drain an already-available result first (non-blocking), so a clean EOF — the reader closing results
	// with records still buffered — is never lost to a concurrent WAL shutdown in the select below.
	select {
	case result, ok := <-it.results:
		return it.deliver(result, ok)
	default:
	}
	// Otherwise wait for the next result, but don't block forever if the WAL is torn down. The reader
	// goroutine watches the same context (see send) and stops producing when it fires, so the results
	// channel would never advance again; surface the shutdown as an error rather than hang.
	select {
	case result, ok := <-it.results:
		return it.deliver(result, ok)
	case <-it.wal.ctx.Done():
		it.done = true
		return false, fmt.Errorf("WAL shut down during iteration: %w", it.wal.ctx.Err())
	}
}

// deliver turns a dequeued reader result (or a closed-channel signal) into a Next return value.
func (it *walIterator) deliver(result iteratorResult, ok bool) (bool, error) {
	if !ok {
		it.done = true
		return false, nil
	}
	if result.err != nil {
		it.done = true
		return false, result.err
	}
	it.resultIndex = result.index
	it.resultPayload = result.payload
	return true, nil
}

func (it *walIterator) Entry() (uint64, []byte) {
	return it.resultIndex, it.resultPayload
}

func (it *walIterator) Close() error {
	it.closeOnce.Do(func() {
		close(it.stop)    // tell the reader to stop if it is mid-read
		<-it.readerExited // wait for it to actually exit before releasing its file handles
		if it.dir != "" {
			// Remove this iterator's hard-link snapshot, freeing any inode it was the last link to. Best-effort:
			// a leftover is reclaimed by the startup blast. The reader has exited, so no handle is open here.
			_ = os.RemoveAll(it.dir)
		}
	})
	it.done = true
	return nil
}

// read is the reader goroutine: it produces records onto the results channel until the WAL is exhausted (then
// closes the channel), a read fails (then sends the error), Close signals a stop, or the WAL context is
// cancelled (see send). It never blocks indefinitely, so it cannot outlive the WAL as a zombie.
func (it *walIterator) read() {
	defer close(it.readerExited)
	for {
		record, ok, err := it.nextRecord()
		if err != nil {
			it.send(iteratorResult{err: err})
			return
		}
		if !ok {
			close(it.results)
			return
		}
		if !it.send(iteratorResult{index: record.index, payload: record.payload}) {
			return // Close signalled a stop
		}
	}
}

// send pushes a result onto the channel, returning false if Close signalled a stop or the WAL was torn down
// first. Watching the WAL context here is what keeps the reader from becoming a zombie: if an iterator is
// orphaned (Iterator aborted via ctx.Done before the caller ever received it, so Close is never called) the
// prefetch buffer eventually fills and this send would otherwise block forever with no one to close stop.
func (it *walIterator) send(result iteratorResult) bool {
	select {
	case it.results <- result:
		return true
	case <-it.stop:
		return false
	case <-it.wal.ctx.Done():
		return false
	}
}

// nextRecord returns the next record in ascending order, advancing across files as needed. It returns
// ok=false once no further records remain.
func (it *walIterator) nextRecord() (walRecord, bool, error) {
	for {
		it.pos++
		if it.pos < len(it.buffer) {
			return it.buffer[it.pos], true, nil
		}
		loaded, err := it.loadNextFile()
		if err != nil {
			return walRecord{}, false, err
		}
		if !loaded {
			return walRecord{}, false, nil
		}
		it.pos = -1
	}
}

// loadNextFile walks the file snapshot from filePos, loading the next file's records (filtered to indices at
// or beyond start) into buffer and advancing filePos. It returns false when the snapshot is exhausted. Sealed
// files entirely below start are skipped without being opened; a file that yields no matching records leaves
// buffer empty (still reported as loaded).
func (it *walIterator) loadNextFile() (bool, error) {
	for {
		if it.filePos >= len(it.files) {
			return false, nil
		}
		f := &it.files[it.filePos]
		it.filePos++
		it.buffer = nil

		if f.lastIndex < it.start {
			continue // entirely below the start index; skip without opening
		}

		handle, err := it.openFile(f)
		if err != nil {
			return false, err
		}

		parsed := parsedFileName{fileSeq: f.fileSeq, firstIndex: f.firstIndex, lastIndex: f.lastIndex, sealed: f.sealed}
		contents, err := readWalFileFromHandle(handle, parsed)
		if err != nil {
			return false, fmt.Errorf("failed to read WAL file (sequence %d) during iteration: %w", f.fileSeq, err)
		}

		if f.sealed {
			// A sealed file is durable and complete, so its content must span the [first, last] range its name
			// promises. Fail loudly on any shortfall (interior corruption) instead of silently under-yielding while
			// Bounds/GetStoredRange keep reporting the full range. This mirrors the check run eagerly at open by
			// validateSealedFiles. The non-sealed mutable snapshot is skipped: it may hold records past maxIndex and
			// a torn tail from concurrent writing, both handled below.
			if err := verifySealedContents(contents, f.fileSeq, f.firstIndex, f.lastIndex); err != nil {
				return false, err
			}
		}

		for _, record := range contents.records {
			if record.index < it.start {
				// Locating the start index is a linear scan over this file's records (and the whole file was just
				// read into memory above). It's wasteful when start lands deep in a large file. If this becomes a
				// hotspot, build a small per-file index (offset by index, like LittDB key files) and seek instead.
				continue
			}
			if record.index > it.maxIndex {
				break // beyond the point-in-time snapshot; records ascend, so nothing further qualifies
			}
			it.buffer = append(it.buffer, record)
		}
		return true, nil
	}
}

// openFile opens a snapshot file from the iterator's private hard-link directory. The hard link keeps the
// underlying inode alive against rotation and pruning, so the open cannot miss it even after the WAL removed
// the file's canonical name. readWalFileFromHandle closes the returned handle after reading.
func (it *walIterator) openFile(f *iteratorFile) (*os.File, error) {
	path := filepath.Join(it.dir, f.name)
	handle, err := os.Open(path) //nolint:gosec // path derived from the writer's file snapshot
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file %s during iteration: %w", f.name, err)
	}
	return handle, nil
}
