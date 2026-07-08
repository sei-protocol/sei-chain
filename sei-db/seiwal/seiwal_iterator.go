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
// is created (see startIterator). Every snapshot file is sealed and immutable: it carries its immutable name
// and is opened lazily by the reader, held against pruning by the iterator's read lease.
type iteratorFile struct {
	fileSeq    uint64
	name       string
	firstIndex uint64
	lastIndex  uint64
}

// walIterator iterates the WAL a record at a time, in ascending index order. A dedicated reader goroutine
// reads WAL files from disk and pushes each record onto a buffered channel; Next simply dequeues. Decoupling
// disk reads from the consumer keeps the reader busy while the consumer works, which matters for startup
// replay speed. The reader loads one file at a time, so its memory use is bounded by a single WAL file plus
// the prefetch buffer.
//
// The set of files to read is snapshotted once at creation (files), so the reader walks it in O(n) without
// re-scanning the directory. Every snapshot file is sealed and immutable (the mutable file is sealed at
// creation, see startIterator), so its contents cannot change under the reader. A read lease (pinnedIndex)
// holds the files the reader needs against concurrent pruning; Close releases it.
type walIterator struct {
	// The WAL this iterator reads from.
	wal *walImpl

	// The lowest index the consumer asked for; records below it are skipped.
	start uint64

	// The index pinned as this iterator's read lease, released on Close.
	pinnedIndex uint64

	// Records produced by the reader goroutine. Closed by the reader on clean EOF.
	results chan iteratorResult

	// Closed by Close to tell the reader goroutine to stop early.
	stop chan struct{}

	// Closed by the reader goroutine when it exits, so Close can wait for it.
	readerExited chan struct{}

	// Ensures the shutdown sequence runs at most once.
	closeOnce sync.Once

	// The index and payload returned by Entry, set by the most recent successful Next. Consumer-owned.
	resultIndex   uint64
	resultPayload []byte

	// Set once iteration is complete. Consumer-owned.
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
// pinnedIndex is the read lease registered on the iterator's behalf, released by Close. files is the snapshot
// of files to read (captured on the writer goroutine). prefetch is the number of records the reader may buffer
// ahead of the consumer.
func newWalIterator(
	wal *walImpl,
	startIndex uint64,
	pinnedIndex uint64,
	files []iteratorFile,
	prefetch uint,
) *walIterator {
	it := &walIterator{
		wal:          wal,
		start:        startIndex,
		pinnedIndex:  pinnedIndex,
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
		<-it.readerExited // wait for it to actually exit before releasing resources
		it.wal.unpinIndex(it.pinnedIndex)
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

		parsed := parsedFileName{fileSeq: f.fileSeq, firstIndex: f.firstIndex, lastIndex: f.lastIndex, sealed: true}
		contents, err := readWalFileFromHandle(handle, parsed)
		if err != nil {
			return false, fmt.Errorf("failed to read WAL file (sequence %d) during iteration: %w", f.fileSeq, err)
		}
		for _, record := range contents.records {
			if record.index < it.start {
				continue
			}
			it.buffer = append(it.buffer, record)
		}
		return true, nil
	}
}

// openFile opens a snapshot file by its immutable sealed name. The read lease keeps the file alive against
// pruning, so the open cannot miss it. readWalFileFromHandle closes the returned handle after reading.
func (it *walIterator) openFile(f *iteratorFile) (*os.File, error) {
	path := filepath.Join(it.wal.config.Path, f.name)
	handle, err := os.Open(path) //nolint:gosec // path derived from the writer's file snapshot
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file %s during iteration: %w", f.name, err)
	}
	return handle, nil
}
