package seiwal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/zbiljic/go-filelock"
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

// iteratorStartRequest asks the writer to construct an iterator. The writer hard-links a point-in-time
// snapshot of the files to read and builds the iterator, all on its own goroutine so the snapshot is
// serialized with rotation/seal/prune.
type iteratorStartRequest struct {
	startIndex uint64
	endIndex   uint64
	reply      chan iteratorStartResponse
}

// The iterator (or an error) produced by the writer in response to an iteratorStartRequest.
type iteratorStartResponse struct {
	iterator *walIterator
	err      error

	// fatal reports whether err left the WAL in a state that requires bricking the pipeline. A non-fatal err (a
	// caller range error, or a non-corrupting filesystem failure while building the snapshot) leaves the WAL
	// usable, so the writer surfaces it to the caller and keeps running.
	fatal bool
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

	// The exclusive advisory lock on the WAL directory, held for this instance's entire lifetime and
	// released by Close(). It prevents a second WAL instance or an offline utility from mutating the same
	// directory concurrently.
	fileLock filelock.TryLockerSafe

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

	// The serial number to assign the next iterator, naming its hard-link snapshot directory
	// (iterator/<serial>/). Mutated only by the writer goroutine.
	nextIteratorSeq uint64
}

// recoverDirectory brings a WAL directory into a clean, consistent on-disk state: it removes crash remnants
// from an interrupted rollback and seals any unsealed file left behind by a prior session. After it returns,
// every record lives in a sealed file whose name matches its content, with no orphans remaining. Shared by
// the WAL constructor and the offline GetRange/PruneAfter utilities, so all three run the same sanity pass.
func recoverDirectory(path string) error {
	if err := util.EnsureDirectoryExists(path, true); err != nil {
		return fmt.Errorf("failed to ensure WAL directory %s: %w", path, err)
	}
	// Blast any hard-link snapshots left by iterators of a prior session; they are ephemeral read-side leases,
	// never part of the durable WAL, so a crash survivor is always safe to remove.
	if err := deleteIteratorLinks(path); err != nil {
		return err
	}
	// Clean up remnants of a rollback swap interrupted by a crash before scanning (see rollbackStraddlingFile):
	// a leftover swap file from an unfinished AtomicWrite, or two sealed files sharing a sequence because the old
	// file was not yet removed. This leaves a set where every sealed sequence is unique and name matches content.
	if err := util.DeleteOrphanedSwapFiles(path); err != nil {
		return fmt.Errorf("failed to delete orphaned swap files: %w", err)
	}
	if err := reconcileRollbackRemnants(path); err != nil {
		return fmt.Errorf("failed to reconcile rollback remnants: %w", err)
	}
	if err := recoverOrphans(path); err != nil {
		return fmt.Errorf("failed to recover orphaned WAL files: %w", err)
	}
	return nil
}

func newWAL(config *Config) (WAL[[]byte], error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid WAL config: %w", err)
	}

	// Take the directory lock before touching any files: recoverDirectory (below) mutates the directory, so
	// we must own it exclusively first. The lock is released by Close(), or here on any open failure.
	if err := util.EnsureDirectoryExists(config.Path, true); err != nil {
		return nil, fmt.Errorf("failed to ensure WAL directory %s: %w", config.Path, err)
	}
	fileLock, err := acquireDirLock(config.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to lock WAL directory %s: %w", config.Path, err)
	}
	defer func() {
		if fileLock != nil {
			releaseDirLock(fileLock, config.Path)
		}
	}()

	if err := recoverDirectory(config.Path); err != nil {
		return nil, err
	}
	// Only the cheap directory-listing scan runs at open: it reads file names, not their contents, and still
	// rejects structural corruption (a gap in the sealed sequence).
	sealedFiles, nextFileSeq, err := scanSealedFiles(config.Path)
	if err != nil {
		return nil, err
	}

	mutable, err := newWalFile(config.Path, nextFileSeq)
	if err != nil {
		return nil, fmt.Errorf("failed to open mutable WAL file: %w", err)
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	senderCtx, senderCancel := context.WithCancelCause(ctx)

	w := &walImpl{
		config:       config,
		fileLock:     fileLock,
		metricAttrs:  walNameAttr(config.Name),
		writerChan:   make(chan any, config.WriteBufferSize),
		ctx:          ctx,
		cancel:       cancel,
		senderCtx:    senderCtx,
		senderCancel: senderCancel,
		mutableFile:  mutable,
		nextFileSeq:  nextFileSeq + 1,
		sealedFiles:  sealedFiles,
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

	// Ownership of the lock has passed to w (released by Close); disarm the open-failure cleanup above.
	fileLock = nil
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

// Iterator returns an iterator over the inclusive index range [startIndex, endIndex]. Construction runs on
// the writer goroutine (see iteratorStartRequest): the writer captures a hard-link snapshot of the files to
// read so later rotation and pruning cannot pull them out from under the iterator, and builds the iterator.
// The snapshot is removed by the iterator's Close.
func (w *walImpl) Iterator(startIndex uint64, endIndex uint64) (Iterator[[]byte], error) {
	reply := make(chan iteratorStartResponse, 1)
	req := iteratorStartRequest{startIndex: startIndex, endIndex: endIndex, reply: reply}
	if err := w.sendToWriter(req); err != nil {
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
		// Release the directory lock only after every goroutine has stopped, so nothing touches the files
		// after another owner could acquire the directory.
		releaseDirLock(w.fileLock, w.config.Path)
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
			resp := w.startIterator(m.startIndex, m.endIndex)
			m.reply <- resp
			// A rejected range and a non-corrupting snapshot I/O failure both leave the WAL healthy; only a
			// failed flush of buffered records desyncs in-memory state from disk and bricks the pipeline.
			if resp.fatal {
				w.fail(resp.err)
				return
			}
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
				return fmt.Errorf(
					"append out of order: index %d is not greater than last written index %d",
					m.index, w.lastWrittenIndex)
			}
		} else if m.index != w.lastWrittenIndex+1 {
			return fmt.Errorf(
				"append out of order: index %d is not contiguous with last written index "+
					"%d (expected %d)",
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
// called when the mutable file holds at least one record (immediately after a size-triggering append), so the
// seal always produces a sealed file rather than removing an empty one.
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
// Pruning is unconditional with respect to live iterators: an iterator holds its own hard links to every file
// it reads (see startIterator), so removing a file's canonical name here only drops one link — the inode
// survives until the iterator's link is removed on Close. Pruning therefore need not know iterators exist.
//
// Iteration stops at the first retained file: index ranges grow toward the back, so once a file reaches
// pruneThrough every later file is kept too.
func (w *walImpl) pruneSealedFiles(pruneThrough uint64) error {
	for {
		front, ok := w.sealedFiles.TryPeekFront()
		if !ok || front.lastIndex >= pruneThrough {
			break
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

// startIterator builds an iterator on the writer goroutine. It captures a point-in-time snapshot by
// hard-linking every file the iterator will read into a private directory (iterator/<serial>/) and bounding it
// at endIndex, which must not exceed the highest index stored now. Running here serializes the snapshot with
// rotation, seal, and prune. The live WAL may then rotate, seal, and prune freely: the iterator reads only its
// own hard links, which keep the underlying inodes alive until Close removes them. The mutable file is flushed
// (not fsynced) so its records are readable through the reader's separate handle — no crash can intervene
// between creation and use, so durability is irrelevant here.
//
// Failures split by whether they leave the WAL usable. A range rejection (ErrIteratorRange) is a caller error,
// and a snapshot failure (MkdirAll/os.Link) touches only the ephemeral iterator/<serial>/ lease directory, so
// both are returned non-fatally and the WAL keeps running. Only a failed mutable-file flush is fatal: it
// poisons the file's buffered writer and leaves in-memory bookkeeping ahead of what is durable, so the caller
// marks the response fatal to brick the pipeline.
func (w *walImpl) startIterator(startIndex uint64, endIndex uint64) iteratorStartResponse {
	if endIndex < startIndex {
		return iteratorStartResponse{
			err: fmt.Errorf(
				"%w: end index %d is below start index %d", ErrIteratorRange, endIndex, startIndex),
		}
	}
	r := w.bounds()
	if !r.ok {
		return iteratorStartResponse{
			err: fmt.Errorf("%w: end index %d requested but the WAL is empty", ErrIteratorRange, endIndex),
		}
	}
	if endIndex > r.last {
		return iteratorStartResponse{
			err: fmt.Errorf(
				"%w: end index %d is beyond the latest stored index %d",
				ErrIteratorRange, endIndex, r.last),
		}
	}
	maxIndex := endIndex

	// Gather the files to read: those whose range overlaps [startIndex, endIndex], plus the mutable file if it
	// holds records in range. Files entirely below startIndex or entirely above endIndex are never opened, so
	// they are not linked. The mutable snapshot's range is capped at maxIndex; the reader drops anything the
	// writer appends past it.
	var sources []iteratorFile
	for _, info := range w.sealedFiles.Iterator() {
		if info.lastIndex < startIndex || info.firstIndex > endIndex {
			continue
		}
		sources = append(sources, iteratorFile{
			fileSeq: info.fileSeq, name: info.name,
			firstIndex: info.firstIndex, lastIndex: info.lastIndex, sealed: true,
		})
	}
	// Flush the mutable file only when it is actually needed to cover the range. When endIndex is fully served
	// by sealed files, the mutable file is left untouched.
	if w.mutableFile.hasRecords && w.mutableFile.lastIndex >= startIndex && w.mutableFile.firstIndex <= endIndex {
		if err := w.mutableFile.flush(false); err != nil {
			return iteratorStartResponse{
				err:   fmt.Errorf("failed to flush mutable file for iterator: %w", err),
				fatal: true,
			}
		}
		sources = append(sources, iteratorFile{
			fileSeq: w.mutableFile.fileSeq, name: unsealedFileName(w.mutableFile.fileSeq),
			firstIndex: w.mutableFile.firstIndex, lastIndex: maxIndex, sealed: false,
		})
	}

	if len(sources) == 0 {
		// Nothing at or above startIndex to read; no snapshot directory needed.
		it := newWalIterator(w, startIndex, maxIndex, "", nil, w.config.IteratorPrefetchSize)
		return iteratorStartResponse{iterator: it}
	}

	dir := iteratorLinkDir(w.config.Path, w.nextIteratorSeq)
	if err := w.linkSnapshot(dir, sources); err != nil {
		_ = os.RemoveAll(dir)
		return iteratorStartResponse{err: err}
	}
	w.nextIteratorSeq++
	it := newWalIterator(w, startIndex, maxIndex, dir, sources, w.config.IteratorPrefetchSize)
	return iteratorStartResponse{iterator: it}
}

// linkSnapshot creates dir and hard-links each source file into it under the source's basename. The links keep
// the underlying inodes alive for the iterator even after the WAL rotates or prunes the originals. No fsync:
// the links are ephemeral read-side leases, reclaimed on Close or, after a crash, at the next open.
func (w *walImpl) linkSnapshot(dir string, sources []iteratorFile) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create iterator snapshot directory %s: %w", dir, err)
	}
	for _, f := range sources {
		src := filepath.Join(w.config.Path, f.name)
		if err := os.Link(src, filepath.Join(dir, f.name)); err != nil {
			return fmt.Errorf("failed to hard-link %s for iterator: %w", f.name, err)
		}
	}
	return nil
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
	w.senderCancel(err)
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
			// This file lies entirely at or below the rollback point; so does every lower-sequence
			// file. Done.
			break
		}
		if parsed.firstIndex > rollbackThrough {
			// Entirely beyond the rollback point: remove the whole file, durably, before the
			// next-lower one.
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
