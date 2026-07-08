package seiwal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/sei-protocol/seilog"
)

var _ WAL = (*walImpl)(nil)

var logger = seilog.NewLogger("db", "seiwal")

// dataToBeWritten carries a framed record from a caller to the writer to be appended.
type dataToBeWritten struct {
	record []byte
	index  uint64
}

// flushRequest asks the writer to flush (and optionally fsync) the mutable file, signaling done when durable.
type flushRequest struct {
	done chan error
}

// rangeQuery asks the writer to report the stored index range.
type rangeQuery struct {
	reply chan storedRange
}

// pruneRequest asks the writer to drop whole sealed files below `through`.
type pruneRequest struct {
	through uint64
}

// closeRequest asks the writer to seal the mutable file and shut down, signaling done when sealed.
type closeRequest struct {
	done chan error
}

// unpinRequest releases a read lease previously registered when an iterator was created.
type unpinRequest struct {
	index uint64
}

// iteratorStartRequest asks the writer to construct an iterator. The writer flushes the mutable file (so the
// iterator observes all prior appends), snapshots the current set of files, registers the read lease, and
// builds the iterator, all on its own goroutine so construction is serialized with rotation/seal/prune.
type iteratorStartRequest struct {
	startIndex uint64
	reply      chan iteratorStartResponse
}

// The iterator (or an error) produced by the writer in response to an iteratorStartRequest.
type iteratorStartResponse struct {
	iterator *walIterator
	err      error
}

// The index range reported by Bounds.
type storedRange struct {
	ok    bool
	first uint64
	last  uint64
}

// Bookkeeping for a sealed WAL file, owned by the writer goroutine.
type sealedFileInfo struct {
	fileSeq    uint64
	name       string
	firstIndex uint64
	lastIndex  uint64
}

// A generic write-ahead log implementation.
type walImpl struct {
	// The configuration this WAL was opened with. Read-only after construction.
	config *Config

	// Callers funnel framed records and control messages through writerChan as a single ordered stream to
	// the writer goroutine.
	writerChan chan any

	// The hard-stop context the writer watches. Cancelled by fail() on a fatal error and by Close() once
	// everything has drained.
	ctx    context.Context
	cancel context.CancelFunc

	// A child of ctx that the writerChan producers watch, cancelled once the writer stops reading so an
	// in-flight or future push aborts rather than deadlocking.
	senderCtx    context.Context
	senderCancel context.CancelFunc

	// Tracks the writer goroutine so Close() can wait for it to exit.
	wg sync.WaitGroup

	// Guarantees the Close() shutdown sequence runs at most once.
	closeOnce sync.Once

	// Set by Close() so subsequent scheduling calls fail fast.
	closed atomic.Bool

	// The first unrecoverable background-goroutine error, surfaced to the caller by Close().
	asyncErr atomic.Pointer[error]

	// Guards the append-ordering state below, which is read/written synchronously in Append (not on the
	// writer goroutine).
	appendMu sync.Mutex
	// The index of the most recently appended record.
	lastAppendIndex uint64
	// Whether any record has been appended (this session or recovered from disk).
	hasAppended bool

	// The following fields are owned exclusively by the writer goroutine.

	// The current mutable file accepting records.
	mutableFile *walFile

	// The sequence number to assign the next mutable file.
	nextFileSeq uint64

	// Sealed files in ascending index order. Rotation appends to the back; pruning removes from the front.
	sealedFiles *util.RandomAccessDeque[*sealedFileInfo]

	// Read leases held by live iterators: record index -> reference count. Pruning will not delete a file
	// whose index range contains a leased index. Mutated only by the writer goroutine.
	indexRefs map[uint64]int
}

func newWAL(config *Config, rollbackThrough *uint64) (WAL, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid WAL config: %w", err)
	}
	if err := util.EnsureDirectoryExists(config.Path, true); err != nil {
		return nil, fmt.Errorf("failed to ensure WAL directory %s: %w", config.Path, err)
	}

	// Clean up remnants of a rollback swap interrupted by a crash before scanning (see rollbackStraddlingFile):
	// a leftover swap file from an unfinished AtomicWrite, or two sealed files sharing a sequence because the old
	// file was not yet removed. This leaves a set where every sealed sequence is unique and name matches content.
	if err := util.DeleteOrphanedSwapFiles(config.Path); err != nil {
		return nil, fmt.Errorf("failed to delete orphaned swap files: %w", err)
	}
	if err := reconcileRollbackRemnants(config.Path); err != nil {
		return nil, fmt.Errorf("failed to reconcile rollback remnants: %w", err)
	}
	if err := recoverOrphans(config.Path); err != nil {
		return nil, fmt.Errorf("failed to recover orphaned WAL files: %w", err)
	}
	if rollbackThrough != nil {
		if err := rollbackDirectory(config.Path, *rollbackThrough); err != nil {
			return nil, fmt.Errorf("failed to roll back WAL beyond index %d: %w", *rollbackThrough, err)
		}
	}

	sealedFiles, nextFileSeq, err := scanSealedFiles(config.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to scan sealed WAL files: %w", err)
	}

	mutable, err := newWalFile(config.Path, nextFileSeq)
	if err != nil {
		return nil, fmt.Errorf("failed to open mutable WAL file: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	senderCtx, senderCancel := context.WithCancel(ctx)

	w := &walImpl{
		config:       config,
		writerChan:   make(chan any, config.WriteBufferSize),
		ctx:          ctx,
		cancel:       cancel,
		senderCtx:    senderCtx,
		senderCancel: senderCancel,
		mutableFile:  mutable,
		nextFileSeq:  nextFileSeq + 1,
		sealedFiles:  sealedFiles,
		indexRefs:    make(map[uint64]int),
	}
	// Recover the append-ordering position from the highest index already on disk.
	if r := w.bounds(); r.ok {
		w.lastAppendIndex = r.last
		w.hasAppended = true
	}

	w.wg.Add(1)
	go w.writerLoop()

	return w, nil
}

// Append frames a record and schedules it for the writer, after enforcing that indices strictly increase.
func (w *walImpl) Append(index uint64, data []byte) error {
	if w.closed.Load() {
		return fmt.Errorf("WAL is closed")
	}

	w.appendMu.Lock()
	if w.hasAppended && index <= w.lastAppendIndex {
		last := w.lastAppendIndex
		w.appendMu.Unlock()
		return fmt.Errorf("append rejected: index %d is not greater than last appended index %d", index, last)
	}
	w.lastAppendIndex = index
	w.hasAppended = true
	w.appendMu.Unlock()

	record := frameRecord(index, data)
	if err := w.sendToWriter(dataToBeWritten{record: record, index: index}); err != nil {
		return fmt.Errorf("failed to schedule append for index %d: %w", index, err)
	}
	return nil
}

// Flush blocks until all previously scheduled appends are durable.
func (w *walImpl) Flush() error {
	done := make(chan error, 1)
	if err := w.sendToWriter(flushRequest{done: done}); err != nil {
		return fmt.Errorf("failed to schedule flush: %w", err)
	}
	select {
	case err := <-done:
		return err // already wrapped by the writer, or nil on success
	case <-w.ctx.Done():
		if err := w.asyncError(); err != nil {
			return fmt.Errorf("flush aborted: %w", err)
		}
		return fmt.Errorf("flush aborted: %w", w.ctx.Err())
	}
}

// Bounds reports the range of record indices stored in the WAL.
func (w *walImpl) Bounds() (bool, uint64, uint64, error) {
	reply := make(chan storedRange, 1)
	if err := w.sendToWriter(rangeQuery{reply: reply}); err != nil {
		return false, 0, 0, fmt.Errorf("failed to schedule bounds query: %w", err)
	}
	select {
	case r := <-reply:
		return r.ok, r.first, r.last, nil
	case <-w.ctx.Done():
		if err := w.asyncError(); err != nil {
			return false, 0, 0, fmt.Errorf("bounds query aborted: %w", err)
		}
		return false, 0, 0, fmt.Errorf("bounds query aborted: %w", w.ctx.Err())
	}
}

// Prune schedules removal of whole sealed files below lowestIndexToKeep. It does not block on completion.
func (w *walImpl) Prune(lowestIndexToKeep uint64) error {
	if err := w.sendToWriter(pruneRequest{through: lowestIndexToKeep}); err != nil {
		return fmt.Errorf("failed to schedule prune below index %d: %w", lowestIndexToKeep, err)
	}
	return nil
}

// Iterator returns an iterator over the WAL starting at startIndex. Construction runs on the writer goroutine
// (see iteratorStartRequest): the writer flushes so all previously scheduled appends are visible, registers a
// read lease so pruning cannot delete files out from under the iterator, and builds the iterator. The lease is
// released by the iterator's Close.
func (w *walImpl) Iterator(startIndex uint64) (Iterator, error) {
	reply := make(chan iteratorStartResponse, 1)
	if err := w.sendToWriter(iteratorStartRequest{startIndex: startIndex, reply: reply}); err != nil {
		return nil, fmt.Errorf("failed to schedule iterator creation: %w", err)
	}
	select {
	case resp := <-reply:
		if resp.err != nil {
			return nil, fmt.Errorf("failed to create iterator: %w", resp.err)
		}
		return resp.iterator, nil
	case <-w.ctx.Done():
		if err := w.asyncError(); err != nil {
			return nil, fmt.Errorf("iterator creation aborted: %w", err)
		}
		return nil, fmt.Errorf("iterator creation aborted: %w", w.ctx.Err())
	}
}

// unpinIndex releases a read lease. Best-effort: if the WAL is already shutting down the lease is moot.
func (w *walImpl) unpinIndex(index uint64) {
	_ = w.sendToWriter(unpinRequest{index: index})
}

// Close flushes pending appends, seals the mutable file, and releases resources.
func (w *walImpl) Close() error {
	var closeErr error
	w.closeOnce.Do(func() {
		w.closed.Store(true)
		done := make(chan error, 1)
		if err := w.sendToWriter(closeRequest{done: done}); err == nil {
			select {
			case closeErr = <-done:
			case <-w.ctx.Done():
			}
		}
		w.wg.Wait()
		w.cancel()
	})
	if err := w.asyncError(); err != nil {
		return fmt.Errorf("WAL closed with error: %w", err)
	}
	return closeErr // already wrapped by the writer, or nil on a clean seal
}

// sendToWriter enqueues a message onto the writer's input channel, aborting if the WAL is shutting down or has
// failed.
func (w *walImpl) sendToWriter(msg any) error {
	select {
	case w.writerChan <- msg:
		return nil
	case <-w.senderCtx.Done():
		if err := w.asyncError(); err != nil {
			return fmt.Errorf("WAL failed: %w", err)
		}
		return fmt.Errorf("WAL is closed")
	}
}

// writerLoop consumes messages, appending records to the mutable file and handling control messages. It owns
// all file bookkeeping and runs on its own goroutine until close or a fatal error.
func (w *walImpl) writerLoop() {
	defer w.wg.Done()
	for {
		var msg any
		select {
		case <-w.ctx.Done():
			return
		case msg = <-w.writerChan:
		}

		switch m := msg.(type) {
		case dataToBeWritten:
			if err := w.appendRecord(m); err != nil {
				w.fail(err)
				return
			}
		case flushRequest:
			m.done <- w.mutableFile.flush(w.config.FsyncOnFlush)
		case rangeQuery:
			m.reply <- w.bounds()
		case pruneRequest:
			if err := w.pruneSealedFiles(m.through); err != nil {
				w.fail(err)
				return
			}
		case iteratorStartRequest:
			m.reply <- w.startIterator(m.startIndex)
		case unpinRequest:
			w.releaseIndex(m.index)
		case closeRequest:
			_, err := w.mutableFile.seal()
			m.done <- err
			// FIFO guarantees every prior append has been processed. Forbid further pushes so any
			// racing/future schedule aborts instead of deadlocking against the now-exiting writer.
			w.senderCancel()
			return
		}
	}
}

// appendRecord appends a record to the mutable file, updates bookkeeping, and rotates once the file exceeds
// the target size. Every record is complete, so any record is a valid rotation boundary.
func (w *walImpl) appendRecord(m dataToBeWritten) error {
	if err := w.mutableFile.writeRecord(m.record, m.index); err != nil {
		return fmt.Errorf("failed to append record for index %d: %w", m.index, err)
	}
	walBytesWritten.Add(w.ctx, int64(len(m.record)))
	walRecordsWritten.Add(w.ctx, 1)

	if w.mutableFile.size >= uint64(w.config.TargetFileSize) {
		if err := w.rotate(); err != nil {
			return fmt.Errorf("failed to rotate after index %d: %w", m.index, err)
		}
	}
	return nil
}

// rotate seals the current mutable file, records its bookkeeping, and opens a fresh mutable file. It is only
// called when the mutable file holds at least one record (immediately after an append, or from sealForIterator
// when it has records), so the seal always produces a sealed file rather than removing an empty one.
func (w *walImpl) rotate() error {
	fileSeq := w.mutableFile.fileSeq
	first := w.mutableFile.firstIndex
	last := w.mutableFile.lastIndex
	sealedName, err := w.mutableFile.seal()
	if err != nil {
		return fmt.Errorf("failed to seal WAL file during rotation: %w", err)
	}
	w.sealedFiles.PushBack(&sealedFileInfo{fileSeq: fileSeq, name: sealedName, firstIndex: first, lastIndex: last})
	walFilesSealed.Add(w.ctx, 1)

	mutable, err := newWalFile(w.config.Path, w.nextFileSeq)
	if err != nil {
		return fmt.Errorf("failed to open new mutable WAL file during rotation: %w", err)
	}
	w.mutableFile = mutable
	w.nextFileSeq++
	return nil
}

// pruneSealedFiles deletes sealed files whose highest index is below pruneThrough. Files are removed
// oldest-first (from the front of the deque) with a directory fsync after each removal, so a crash mid-prune
// leaves a contiguous suffix of files rather than a gap in the sequence. The mutable file is never pruned.
//
// A live iterator holds a read lease at some index R and may still read every record from R onward, so no file
// whose range reaches R or higher may be removed. A file [first, last] is needed iff it overlaps [R, ∞), i.e.
// iff last >= R. Comparing the lowest live reservation against each file's last index (rather than testing
// whether the reservation falls inside a file's range) protects exactly the files an iterator can still open —
// even when the reservation lands in a gap between files or strictly inside a file's range. Because
// reservations never fall below the lowest stored index (see pinLowestReadableIndex), a file left below the
// lowest reservation is one the iterator has already moved past and can safely be dropped.
//
// Iteration stops at the first retained file: index ranges grow toward the back, so once a file is kept (by
// pruneThrough or by the lowest reservation) every later file is kept too.
func (w *walImpl) pruneSealedFiles(pruneThrough uint64) error {
	// Reservations are mutated only on this (the writer) goroutine, so the lowest reservation is stable for the
	// duration of this prune and can be computed once.
	reservation, hasReservation := w.lowestReservation()
	for {
		front, ok := w.sealedFiles.TryPeekFront()
		if !ok || front.lastIndex >= pruneThrough {
			break
		}
		if hasReservation && front.lastIndex >= reservation {
			break // a live iterator may still read this file (or a later one); keep it and everything after
		}
		path := filepath.Join(w.config.Path, front.name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to prune WAL file %s: %w", path, err)
		}
		if err := util.SyncParentPath(path); err != nil {
			return fmt.Errorf("failed to fsync directory after pruning %s: %w", path, err)
		}
		w.sealedFiles.PopFront()
		walFilesPruned.Add(w.ctx, 1)
	}
	return nil
}

// startIterator builds an iterator on the writer goroutine. It first seals the mutable file (see
// sealForIterator) so every record written so far lives in an immutable sealed file, then snapshots the sealed
// files in ascending index order, registers the read lease, and constructs the iterator (which launches its
// reader goroutine). Running here serializes construction with rotation, seal, and prune, so the snapshot is a
// consistent point-in-time view: every file the iterator reads is sealed and immutable, opened lazily by name
// and protected from pruning by the lease, so its contents cannot change underneath the reader.
func (w *walImpl) startIterator(startIndex uint64) iteratorStartResponse {
	if err := w.sealForIterator(); err != nil {
		return iteratorStartResponse{err: fmt.Errorf("failed to seal mutable file before creating iterator: %w", err)}
	}

	files := make([]iteratorFile, 0, w.sealedFiles.Size())
	for _, info := range w.sealedFiles.Iterator() {
		files = append(files, iteratorFile{
			fileSeq:    info.fileSeq,
			name:       info.name,
			firstIndex: info.firstIndex,
			lastIndex:  info.lastIndex,
		})
	}

	pinned := w.pinLowestReadableIndex(startIndex)
	it := newWalIterator(w, startIndex, pinned, files, w.config.IteratorPrefetchSize)
	return iteratorStartResponse{iterator: it}
}

// sealForIterator seals the mutable file so a newly-created iterator sees a snapshot that cannot change
// underneath it: after this call every record lives in an immutable sealed file. It is a no-op when the
// mutable file holds no records — the iterator reads only sealed files, so an empty mutable file is simply
// left in place.
func (w *walImpl) sealForIterator() error {
	if !w.mutableFile.hasRecords {
		return nil
	}
	if err := w.rotate(); err != nil {
		return fmt.Errorf("failed to seal mutable file: %w", err)
	}
	return nil
}

// pinLowestReadableIndex records a read lease and returns the pinned index. An iterator reads records at or
// above startIndex but never below the oldest record actually stored, so the lease is clamped up to that: a
// stale low start must not pin files that no longer exist (or wedge pruning forever). Clamping to the oldest
// stored index also establishes the invariant pruneSealedFiles relies on: a reservation never falls below the
// lowest stored index, so a file entirely below the lowest reservation is one every iterator has moved past.
func (w *walImpl) pinLowestReadableIndex(startIndex uint64) uint64 {
	pinned := startIndex
	if r := w.bounds(); r.ok && r.first > pinned {
		pinned = r.first
	}
	w.indexRefs[pinned]++
	return pinned
}

// releaseIndex drops one reference to a leased index, forgetting it once the count reaches zero.
func (w *walImpl) releaseIndex(index uint64) {
	if w.indexRefs[index] <= 1 {
		delete(w.indexRefs, index)
		return
	}
	w.indexRefs[index]--
}

// lowestReservation returns the smallest index currently leased by a live iterator, and ok=false when no lease
// is held. A lease at index R means some iterator may still read records at or above R, so every sealed file
// whose range reaches R or higher must be retained by pruning.
func (w *walImpl) lowestReservation() (uint64, bool) {
	var lowest uint64
	found := false
	for index := range w.indexRefs {
		if !found || index < lowest {
			lowest = index
			found = true
		}
	}
	return lowest, found
}

// bounds reports the range of record indices across all files. Owned by the writer goroutine.
func (w *walImpl) bounds() storedRange {
	var r storedRange

	// The highest index is in the mutable file if it has records, otherwise in the newest sealed file.
	if w.mutableFile.hasRecords {
		r = storedRange{ok: true, last: w.mutableFile.lastIndex}
	} else if back, ok := w.sealedFiles.TryPeekBack(); ok {
		r = storedRange{ok: true, last: back.lastIndex}
	} else {
		return storedRange{} // nothing stored yet
	}

	// The lowest index is in the oldest sealed file if any, otherwise in the mutable file.
	if front, ok := w.sealedFiles.TryPeekFront(); ok {
		r.first = front.firstIndex
	} else {
		r.first = w.mutableFile.firstIndex
	}
	return r
}

// fail records the first fatal background error and triggers shutdown of the pipeline.
func (w *walImpl) fail(err error) {
	w.asyncErr.CompareAndSwap(nil, &err)
	w.cancel()
	logger.Error("WAL encountered a fatal error", "err", err)
}

// asyncError returns the first fatal background error, or nil if none occurred.
func (w *walImpl) asyncError() error {
	if p := w.asyncErr.Load(); p != nil {
		return *p
	}
	return nil
}

// recoverOrphans seals any unsealed WAL files left behind by a crash.
func recoverOrphans(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("failed to read WAL directory %s: %w", directory, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		parsed, ok := parseFileName(entry.Name())
		if !ok || parsed.sealed {
			continue
		}
		if err := sealOrphanFile(directory, entry.Name()); err != nil {
			return fmt.Errorf("failed to seal orphan %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// rollbackDirectory drops all records beyond rollbackThrough from the sealed files. Assumes orphans are already
// sealed. Files are processed highest-sequence-first: the files entirely beyond the rollback point (a suffix of
// the sequence) are removed one at a time, each removal made durable before the next, and finally the single
// file straddling the rollback point is truncated. This ordering guarantees that a crash mid-rollback always
// leaves a contiguous prefix of files — never a gap that scanSealedFiles would reject — mirroring the
// contiguous-suffix guarantee that pruning provides from the other end.
func rollbackDirectory(directory string, rollbackThrough uint64) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("failed to read WAL directory %s: %w", directory, err)
	}

	sealed := make([]parsedFileName, 0, len(entries))
	names := make(map[uint64]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		parsed, ok := parseFileName(entry.Name())
		if !ok || !parsed.sealed {
			continue
		}
		sealed = append(sealed, parsed)
		names[parsed.fileSeq] = entry.Name()
	}
	sort.Slice(sealed, func(i int, j int) bool { return sealed[i].fileSeq > sealed[j].fileSeq })

	for _, parsed := range sealed {
		if parsed.lastIndex <= rollbackThrough {
			// This file lies entirely at or below the rollback point; so does every lower-sequence file. Done.
			break
		}
		if parsed.firstIndex > rollbackThrough {
			// Entirely beyond the rollback point: remove the whole file, durably, before the next-lower one.
			if err := removeAndSyncDir(directory, names[parsed.fileSeq]); err != nil {
				return fmt.Errorf("failed to roll back %s: %w", names[parsed.fileSeq], err)
			}
			continue
		}
		// Straddles the rollback point: truncate away the records beyond it. This is the last file to process.
		if err := rollbackStraddlingFile(directory, names[parsed.fileSeq], rollbackThrough); err != nil {
			return fmt.Errorf("failed to roll back %s: %w", names[parsed.fileSeq], err)
		}
	}
	return nil
}

// scanSealedFiles loads the sealed files in a directory into an ascending-order deque and returns the sequence
// to assign the next mutable file (one past the highest sealed sequence, or 0 when there are none). File
// sequences must be contiguous: a gap means a sealed file went missing, which is unrecoverable corruption, so
// this fails with an informative error rather than silently leaving a hole in the index sequence.
func scanSealedFiles(directory string) (*util.RandomAccessDeque[*sealedFileInfo], uint64, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read WAL directory %s: %w", directory, err)
	}

	parsed := make([]parsedFileName, 0, len(entries))
	names := make(map[uint64]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		p, ok := parseFileName(entry.Name())
		if !ok || !p.sealed {
			continue
		}
		parsed = append(parsed, p)
		names[p.fileSeq] = entry.Name()
	}
	sort.Slice(parsed, func(i int, j int) bool { return parsed[i].fileSeq < parsed[j].fileSeq })

	sealedFiles := util.NewRandomAccessDeque[*sealedFileInfo](uint64(len(parsed)))
	var nextFileSeq uint64
	for i, p := range parsed {
		if i > 0 && p.fileSeq != parsed[i-1].fileSeq+1 {
			return nil, 0, fmt.Errorf(
				"WAL is corrupt: sealed file sequences are not contiguous (gap between %d and %d)",
				parsed[i-1].fileSeq, p.fileSeq)
		}
		sealedFiles.PushBack(&sealedFileInfo{
			fileSeq: p.fileSeq, name: names[p.fileSeq], firstIndex: p.firstIndex, lastIndex: p.lastIndex})
		nextFileSeq = p.fileSeq + 1
	}
	return sealedFiles, nextFileSeq, nil
}
