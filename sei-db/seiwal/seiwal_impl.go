package seiwal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/sei-protocol/seilog"
)

var _ WAL[[]byte] = (*walImpl)(nil)

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

	// The measurement option tagging this instance's metrics with its name. Read-only after construction.
	metricAttrs metric.MeasurementOption

	// Callers funnel framed records and control messages through writerChan as a single ordered stream to
	// the writer goroutine.
	writerChan chan any

	// The hard-stop context the writer watches. Cancelled by fail() with the fatal error as its cause, and
	// by Close() (with a nil cause) once everything has drained. The cause carries the fatal error to
	// callers, so no separate error field is needed.
	ctx context.Context
	// Cancels ctx, tearing down the writer goroutine, recording the fatal error (or nil) as the cause.
	cancel context.CancelCauseFunc

	// A child of ctx that the writerChan producers watch, cancelled once the writer stops reading so an
	// in-flight or future push aborts rather than deadlocking.
	senderCtx context.Context
	// Cancels senderCtx.
	senderCancel context.CancelCauseFunc

	// Tracks the writer and queue-depth sampler goroutines so Close() can wait for them to exit.
	wg sync.WaitGroup

	// Closed by Close() to stop the queue-depth sampler goroutine.
	samplerStop chan struct{}

	// Guarantees the Close() shutdown sequence runs at most once.
	closeOnce sync.Once

	// Set by Close() so subsequent scheduling calls fail fast. Plain: calling any method after Close is a
	// contract violation, so this need not be atomic.
	closed bool

	// The index of the most recently appended record.
	lastAppendIndex uint64

	// Whether any record has been appended (this session or recovered from disk).
	hasAppended bool

	// The following fields are owned exclusively by the writer goroutine.

	// The index of the most recently written record. A writer-owned backstop that rejects out-of-order
	// records that slip past the caller-side gate (e.g. under concurrent misuse), turning silent
	// corruption into a fatal error.
	lastWrittenIndex uint64

	// Whether any record has been written (this session or recovered from disk).
	hasWritten bool

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

// NewWAL opens (or creates) a byte-oriented WAL in the configured directory, recovering any files left
// behind by a previous session. Operates on []byte payloads.
func NewWAL(config *Config) (WAL[[]byte], error) {
	return newWAL(config, nil)
}

// NewWALWithRollback opens a byte-oriented WAL and deletes all records with an index greater than
// rollbackIndex before returning, so the WAL contains no record with an index greater than rollbackIndex.
func NewWALWithRollback(config *Config, rollbackIndex uint64) (WAL[[]byte], error) {
	return newWAL(config, &rollbackIndex)
}

func newWAL(config *Config, rollbackThrough *uint64) (WAL[[]byte], error) {
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
	if err := validateSealedFiles(config.Path, sealedFiles); err != nil {
		return nil, fmt.Errorf("corrupt sealed WAL file: %w", err)
	}

	mutable, err := newWalFile(config.Path, nextFileSeq)
	if err != nil {
		return nil, fmt.Errorf("failed to open mutable WAL file: %w", err)
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	senderCtx, senderCancel := context.WithCancelCause(ctx)

	w := &walImpl{
		config:       config,
		metricAttrs:  walNameAttr(config.Name),
		writerChan:   make(chan any, config.WriteBufferSize),
		ctx:          ctx,
		cancel:       cancel,
		senderCtx:    senderCtx,
		senderCancel: senderCancel,
		mutableFile:  mutable,
		nextFileSeq:  nextFileSeq + 1,
		sealedFiles:  sealedFiles,
		indexRefs:    make(map[uint64]int),
		samplerStop:  make(chan struct{}),
	}
	// Recover the append-ordering position from the highest index already on disk.
	if r := w.bounds(); r.ok {
		w.lastAppendIndex = r.last
		w.hasAppended = true
		w.lastWrittenIndex = r.last
		w.hasWritten = true
	}

	w.wg.Add(1)
	go w.writerLoop()

	if config.MetricsSampleInterval > 0 {
		w.wg.Add(1)
		go w.sampleQueueDepth(config.MetricsSampleInterval)
	}

	return w, nil
}

// sampleQueueDepth periodically records the writer channel's buffered depth until Close stops it (samplerStop)
// or a fatal shutdown cancels ctx.
func (w *walImpl) sampleQueueDepth(interval time.Duration) {
	defer w.wg.Done()
	attrs := queueDepthAttrs(w.config.Name, "writer")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.samplerStop:
			return
		case <-ticker.C:
			walQueueDepth.Record(w.ctx, int64(len(w.writerChan)), attrs)
		}
	}
}

// Append frames a record and schedules it for the writer, after enforcing that indices strictly increase.
func (w *walImpl) Append(index uint64, data []byte) error {
	if w.closed {
		return fmt.Errorf("WAL is closed")
	}

	if w.hasAppended {
		if w.config.PermitGaps {
			if index <= w.lastAppendIndex {
				return fmt.Errorf(
					"append rejected: index %d is not greater than last appended index %d",
					index, w.lastAppendIndex)
			}
		} else if index != w.lastAppendIndex+1 {
			return fmt.Errorf(
				"append rejected: index %d is not contiguous with last appended index %d (expected %d)",
				index, w.lastAppendIndex, w.lastAppendIndex+1)
		}
	}
	w.lastAppendIndex = index
	w.hasAppended = true

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

// PruneBefore schedules removal of whole sealed files below lowestIndexToKeep. It does not block on completion.
func (w *walImpl) PruneBefore(lowestIndexToKeep uint64) error {
	if err := w.sendToWriter(pruneRequest{through: lowestIndexToKeep}); err != nil {
		return fmt.Errorf("failed to schedule prune below index %d: %w", lowestIndexToKeep, err)
	}
	return nil
}

// Iterator returns an iterator over the WAL starting at startIndex. Construction runs on the writer goroutine
// (see iteratorStartRequest): the writer flushes so all previously scheduled appends are visible, registers a
// read lease so pruning cannot delete files out from under the iterator, and builds the iterator. The lease is
// released by the iterator's Close.
func (w *walImpl) Iterator(startIndex uint64) (Iterator[[]byte], error) {
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
		w.closed = true
		close(w.samplerStop) // stop the queue-depth sampler before waiting for goroutines
		done := make(chan error, 1)
		if err := w.sendToWriter(closeRequest{done: done}); err == nil {
			select {
			case closeErr = <-done:
			case <-w.ctx.Done():
			}
		}
		w.wg.Wait()
		w.cancel(nil) // a clean close carries no fatal cause; a prior fail() already recorded one
	})
	if err := w.asyncError(); err != nil {
		return fmt.Errorf("WAL closed with error: %w", err)
	}
	return closeErr // already wrapped by the writer, or nil on a clean seal
}

// sendToWriter enqueues a message onto the writer's input channel, aborting if the WAL is shutting down or has
// failed.
func (w *walImpl) sendToWriter(msg any) error {
	// Prioritize shutdown: if the sender context is already done, never race the send case of the select
	// below, which could otherwise enqueue onto a stopped writer's buffer and silently drop the record.
	select {
	case <-w.senderCtx.Done():
		return w.senderErr()
	default:
	}
	select {
	case w.writerChan <- msg:
		return nil
	case <-w.senderCtx.Done():
		return w.senderErr()
	}
}

// senderErr reports why a send was aborted: the fatal cause if the WAL bricked, or a plain closed error if it
// was shut down normally.
func (w *walImpl) senderErr() error {
	if cause := context.Cause(w.senderCtx); cause != nil && cause != context.Canceled {
		return fmt.Errorf("WAL failed: %w", cause)
	}
	return fmt.Errorf("WAL is closed")
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
			err := w.mutableFile.flush(w.config.FsyncOnFlush)
			m.done <- err
			if err != nil {
				w.fail(err)
				return
			}
		case rangeQuery:
			m.reply <- w.bounds()
		case pruneRequest:
			if err := w.pruneSealedFiles(m.through); err != nil {
				w.fail(err)
				return
			}
		case iteratorStartRequest:
			resp := w.startIterator(m.startIndex)
			m.reply <- resp
			if resp.err != nil {
				w.fail(resp.err)
				return
			}
		case unpinRequest:
			w.releaseIndex(m.index)
		case closeRequest:
			_, err := w.mutableFile.seal()
			m.done <- err
			// FIFO guarantees every prior append has been processed. Forbid further pushes so any
			// racing/future schedule aborts instead of deadlocking against the now-exiting writer.
			w.senderCancel(nil) // normal shutdown, not a failure
			return
		}
	}
}

// appendRecord appends a record to the mutable file, updates bookkeeping, and rotates once the file exceeds
// the target size. Every record is complete, so any record is a valid rotation boundary.
func (w *walImpl) appendRecord(m dataToBeWritten) error {
	// Authoritative ordering check: the caller-side gate in Append can be bypassed by concurrent callers
	// (the WAL is documented single-threaded), so re-assert strict increase here on the one writer
	// goroutine to reject a reordered record rather than write a file with inverted index bounds.
	if w.hasWritten {
		if w.config.PermitGaps {
			if m.index <= w.lastWrittenIndex {
				return fmt.Errorf("append out of order: index %d is not greater than last written index %d",
					m.index, w.lastWrittenIndex)
			}
		} else if m.index != w.lastWrittenIndex+1 {
			return fmt.Errorf(
				"append out of order: index %d is not contiguous with last written index %d (expected %d)",
				m.index, w.lastWrittenIndex, w.lastWrittenIndex+1)
		}
	}
	if err := w.mutableFile.writeRecord(m.record, m.index); err != nil {
		return fmt.Errorf("failed to append record for index %d: %w", m.index, err)
	}
	w.lastWrittenIndex = m.index
	w.hasWritten = true
	walBytesWritten.Add(w.ctx, int64(len(m.record)), w.metricAttrs)
	walRecordsWritten.Add(w.ctx, 1, w.metricAttrs)

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
	walFilesSealed.Add(w.ctx, 1, w.metricAttrs)

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
		walFilesPruned.Add(w.ctx, 1, w.metricAttrs)
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

	pinned, hasPin := w.pinLowestReadableIndex(startIndex)
	it := newWalIterator(w, startIndex, pinned, hasPin, files, w.config.IteratorPrefetchSize)
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

// pinLowestReadableIndex records a read lease at index, clamped up to the oldest stored index so no reservation
// ever falls below it (the invariant pruneSealedFiles relies on). pinned is false when nothing is stored, in
// which case no lease is registered and the caller must not release index.
func (w *walImpl) pinLowestReadableIndex(startIndex uint64) (index uint64, pinned bool) {
	r := w.bounds()
	if !r.ok {
		return 0, false
	}
	index = startIndex
	if r.first > index {
		index = r.first
	}
	w.indexRefs[index]++
	return index, true
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

// fail records the first fatal background error and triggers shutdown of the pipeline. The error is recorded
// as the cancellation cause of ctx, so callers observe it via asyncError / context.Cause.
func (w *walImpl) fail(err error) {
	w.cancel(err) // the first cancel wins, so the first fatal error is the one retained
	if cerr := w.mutableFile.close(); cerr != nil {
		logger.Error("failed to close mutable WAL file after fatal error", "err", cerr)
	}
	logger.Error("WAL encountered a fatal error", "err", err)
}

// asyncError returns the first fatal background error, or nil if the WAL is healthy or was closed normally.
func (w *walImpl) asyncError() error {
	if cause := context.Cause(w.ctx); cause != nil && cause != context.Canceled {
		return cause
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

// validateSealedFiles reads and checksums every sealed file, confirming each file's content exactly covers the
// [first, last] index range its name promises. This surfaces corruption (bit-rot, truncation) at open — where
// it demands human intervention — rather than lazily at iteration time, by which point Bounds/GetStoredRange
// would already be reporting a range that iteration cannot deliver. Cost is O(total sealed bytes): every sealed
// file is read and CRC-verified on open.
func validateSealedFiles(directory string, sealedFiles *util.RandomAccessDeque[*sealedFileInfo]) error {
	for _, info := range sealedFiles.Iterator() {
		contents, err := readWalFile(filepath.Join(directory, info.name))
		if err != nil {
			return fmt.Errorf("failed to read sealed WAL file %s: %w", info.name, err)
		}
		if err := verifySealedContents(contents, info.fileSeq, info.firstIndex, info.lastIndex); err != nil {
			return err
		}
	}
	return nil
}
