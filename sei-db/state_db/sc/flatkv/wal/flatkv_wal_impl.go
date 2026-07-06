package wal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/seilog"
)

var _ FlatKVWAL = (*flatKVWalImpl)(nil)

var logger = seilog.NewLogger("db", "state-db", "sc", "flatkv", "wal")

// dataToBeSerialized carries an entry from a caller to the serializer to be serialized.
type dataToBeSerialized struct {
	entry *FlatKVWalEntry
}

// dataToBeWritten carries a framed record from the serializer to the writer to be appended.
type dataToBeWritten struct {
	record      []byte
	blockNumber uint64
	endOfBlock  bool
}

// flushRequest asks the writer to flush (and optionally fsync) the mutable file, signaling done when durable.
type flushRequest struct {
	done chan error
}

// rangeQuery asks the writer to report the stored block range.
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
	block uint64
}

// iteratorStartRequest asks the writer to construct an iterator. The writer flushes the mutable file (so the
// iterator observes all prior writes), snapshots the current set of files, registers the read lease, and builds
// the iterator, all on its own goroutine so construction is serialized with rotation/seal/prune.
type iteratorStartRequest struct {
	startBlock uint64
	reply      chan iteratorStartResponse
}

// The iterator (or an error) produced by the writer in response to an iteratorStartRequest.
type iteratorStartResponse struct {
	iterator *walIterator
	err      error
}

// The block range reported by GetStoredRange.
type storedRange struct {
	ok    bool
	start uint64
	end   uint64
}

// Bookkeeping for a sealed WAL file, owned by the writer goroutine.
type sealedFileInfo struct {
	index      uint64
	name       string
	firstBlock uint64
	lastBlock  uint64
}

// A standard flatKV WAL implementation.
type flatKVWalImpl struct {
	// The configuration this WAL was opened with. Read-only after construction.
	config *FlatKVWALConfig

	//	caller ──serializerChan──▶ serializer ──writerChan──▶ writer

	// Caller entry points funnel through serializerChan as a single ordered stream to the serializer.
	serializerChan chan any

	// The serializer forwards serialized records and control messages to the writer over writerChan.
	writerChan chan any

	// The hard-stop context the serializer and writer watch. Cancelled by fail() on a fatal error and by
	// Close() once everything has drained.
	ctx    context.Context
	cancel context.CancelFunc

	// A child of ctx that the serializerChan producers watch, cancelled once the serializer stops reading so an
	// in-flight or future push aborts rather than deadlocking.
	senderCtx    context.Context
	senderCancel context.CancelFunc

	// Tracks the serializer and writer goroutines so Close() can wait for them to exit.
	wg sync.WaitGroup

	// Guarantees the Close() shutdown sequence runs at most once.
	closeOnce sync.Once

	// Set by Close() so subsequent scheduling calls fail fast.
	closed atomic.Bool

	// The first unrecoverable background-goroutine error, surfaced to the caller by Close().
	asyncErr atomic.Pointer[error]

	// Guards the write-ordering contract state below, which is read/written synchronously in Write and
	// SignalEndOfBlock (not on the background goroutines).
	mu sync.Mutex
	// The block number of the most recent Write or SignalEndOfBlock.
	currentBlock uint64
	// Whether currentBlock has been finalized by SignalEndOfBlock.
	currentBlockEnded bool
	// Whether any block has been observed (this session or recovered from disk).
	hasCurrentBlock bool

	// The following fields are owned exclusively by the writer goroutine.

	// The current mutable file accepting records.
	mutableFile *walFile

	// The index to assign the next mutable file.
	nextIndex uint64

	// Sealed files in ascending block order. Rotation appends to the back; pruning removes from the front.
	sealedFiles *util.RandomAccessDeque[*sealedFileInfo]

	// Read leases held by live iterators: block number -> reference count. Pruning will not delete a file
	// whose block range contains a leased block. Mutated only by the writer goroutine.
	blockRefs map[uint64]int
}

// NewFlatKVWAL opens (or creates) a flatKV WAL in the configured directory, recovering any files left behind
// by a previous session.
func NewFlatKVWAL(config *FlatKVWALConfig) (FlatKVWAL, error) {
	return newFlatKVWal(config, nil)
}

// NewFlatKVWALWithRollback opens a flatKV WAL and deletes all data for blocks beyond rollbackBlockNumber
// before returning, so the WAL contains no block greater than rollbackBlockNumber.
func NewFlatKVWALWithRollback(config *FlatKVWALConfig, rollbackBlockNumber uint64) (FlatKVWAL, error) {
	return newFlatKVWal(config, &rollbackBlockNumber)
}

func newFlatKVWal(config *FlatKVWALConfig, rollbackThrough *uint64) (FlatKVWAL, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid flatKV WAL config: %w", err)
	}
	if err := util.EnsureDirectoryExists(config.Path, true); err != nil {
		return nil, fmt.Errorf("failed to ensure WAL directory %s: %w", config.Path, err)
	}

	// Clean up remnants of a rollback swap interrupted by a crash before scanning (see rollbackStraddlingFile):
	// a leftover swap file from an unfinished AtomicWrite, or two sealed files sharing an index because the old
	// file was not yet removed. This leaves a set where every sealed index is unique and name matches content.
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
			return nil, fmt.Errorf("failed to roll back WAL beyond block %d: %w", *rollbackThrough, err)
		}
	}

	sealedFiles, nextIndex, err := scanSealedFiles(config.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to scan sealed WAL files: %w", err)
	}

	mutable, err := newWalFile(config.Path, nextIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to open mutable WAL file: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	senderCtx, senderCancel := context.WithCancel(ctx)

	w := &flatKVWalImpl{
		config:         config,
		serializerChan: make(chan any, config.RequestBufferSize),
		writerChan:     make(chan any, config.WriteBufferSize),
		ctx:            ctx,
		cancel:         cancel,
		senderCtx:      senderCtx,
		senderCancel:   senderCancel,
		mutableFile:    mutable,
		nextIndex:      nextIndex + 1,
		sealedFiles:    sealedFiles,
		blockRefs:      make(map[uint64]int),
	}
	// Recover the write-ordering position from the last complete block already on disk.
	if r := w.blockRange(); r.ok {
		w.currentBlock = r.end
		w.currentBlockEnded = true
		w.hasCurrentBlock = true
	}

	w.wg.Add(2)
	go w.serializerLoop()
	go w.writerLoop()

	return w, nil
}

// Write schedules a changeset record for the given block number.
func (w *flatKVWalImpl) Write(blockNumber uint64, cs []*proto.NamedChangeSet) error {
	if w.closed.Load() {
		return fmt.Errorf("flatKV WAL is closed")
	}
	if err := w.enforceWriteOrdering(blockNumber); err != nil {
		return fmt.Errorf("write rejected: %w", err)
	}
	if err := w.sendToSerializer(dataToBeSerialized{entry: NewFlatKVWalEntry(blockNumber, cs)}); err != nil {
		return fmt.Errorf("failed to schedule write for block %d: %w", blockNumber, err)
	}
	return nil
}

// SignalEndOfBlock schedules an end-of-block marker for the current block.
func (w *flatKVWalImpl) SignalEndOfBlock() error {
	if w.closed.Load() {
		return fmt.Errorf("flatKV WAL is closed")
	}

	w.mu.Lock()
	if !w.hasCurrentBlock || w.currentBlockEnded {
		w.mu.Unlock()
		return fmt.Errorf("no block in progress to end")
	}
	blockNumber := w.currentBlock
	w.currentBlockEnded = true
	w.mu.Unlock()

	if err := w.sendToSerializer(dataToBeSerialized{entry: NewFlatKVEndOfBlockEntry(blockNumber)}); err != nil {
		return fmt.Errorf("failed to schedule end-of-block for block %d: %w", blockNumber, err)
	}
	return nil
}

// enforceWriteOrdering rejects a Write that violates the block-ordering rules (no decreasing block numbers; no
// advancing to a new block before the current one is ended) and records the new position when it is allowed.
func (w *flatKVWalImpl) enforceWriteOrdering(blockNumber uint64) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.hasCurrentBlock {
		w.currentBlock = blockNumber
		w.currentBlockEnded = false
		w.hasCurrentBlock = true
		return nil
	}
	if blockNumber < w.currentBlock {
		return fmt.Errorf("block number %d is less than the current block number %d", blockNumber, w.currentBlock)
	}
	if blockNumber == w.currentBlock {
		if w.currentBlockEnded {
			return fmt.Errorf("block number %d has already ended; cannot write more changes to it", blockNumber)
		}
		return nil
	}
	// blockNumber > currentBlock
	if !w.currentBlockEnded {
		return fmt.Errorf(
			"cannot write block %d before calling SignalEndOfBlock for block %d", blockNumber, w.currentBlock)
	}
	w.currentBlock = blockNumber
	w.currentBlockEnded = false
	return nil
}

// Flush blocks until all previously scheduled writes are durable.
func (w *flatKVWalImpl) Flush() error {
	done := make(chan error, 1)
	if err := w.sendToSerializer(flushRequest{done: done}); err != nil {
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

// GetStoredRange reports the range of complete blocks stored in the WAL.
func (w *flatKVWalImpl) GetStoredRange() (bool, uint64, uint64, error) {
	reply := make(chan storedRange, 1)
	if err := w.sendToSerializer(rangeQuery{reply: reply}); err != nil {
		return false, 0, 0, fmt.Errorf("failed to schedule stored-range query: %w", err)
	}
	select {
	case r := <-reply:
		return r.ok, r.start, r.end, nil
	case <-w.ctx.Done():
		if err := w.asyncError(); err != nil {
			return false, 0, 0, fmt.Errorf("stored-range query aborted: %w", err)
		}
		return false, 0, 0, fmt.Errorf("stored-range query aborted: %w", w.ctx.Err())
	}
}

// Prune schedules removal of whole sealed files below lowestBlockNumberToKeep. It does not block on completion.
func (w *flatKVWalImpl) Prune(lowestBlockNumberToKeep uint64) error {
	if err := w.sendToSerializer(pruneRequest{through: lowestBlockNumberToKeep}); err != nil {
		return fmt.Errorf("failed to schedule prune below block %d: %w", lowestBlockNumberToKeep, err)
	}
	return nil
}

// Iterator returns an iterator over the WAL starting at startingBlockNumber. Construction runs on the writer
// goroutine (see iteratorStartRequest): the writer flushes so all previously scheduled writes are visible,
// registers a read lease so pruning cannot delete files out from under the iterator, and builds the iterator.
// The lease is released by the iterator's Close.
func (w *flatKVWalImpl) Iterator(startingBlockNumber uint64) (FlatKVWalIterator, error) {
	reply := make(chan iteratorStartResponse, 1)
	if err := w.sendToSerializer(iteratorStartRequest{startBlock: startingBlockNumber, reply: reply}); err != nil {
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

// unpinBlock releases a read lease. Best-effort: if the WAL is already shutting down the lease is moot.
func (w *flatKVWalImpl) unpinBlock(block uint64) {
	_ = w.sendToSerializer(unpinRequest{block: block})
}

// Close flushes pending writes, seals the mutable file, and releases resources.
func (w *flatKVWalImpl) Close() error {
	var closeErr error
	w.closeOnce.Do(func() {
		w.closed.Store(true)
		done := make(chan error, 1)
		if err := w.sendToSerializer(closeRequest{done: done}); err == nil {
			select {
			case closeErr = <-done:
			case <-w.ctx.Done():
			}
		}
		w.wg.Wait()
		w.cancel()
	})
	if err := w.asyncError(); err != nil {
		return fmt.Errorf("flatKV WAL closed with error: %w", err)
	}
	return closeErr // already wrapped by the writer, or nil on a clean seal
}

// sendToSerializer enqueues a message onto the serializer's input channel, aborting if the WAL is
// shutting down or has failed.
func (w *flatKVWalImpl) sendToSerializer(msg any) error {
	select {
	case w.serializerChan <- msg:
		return nil
	case <-w.senderCtx.Done():
		if err := w.asyncError(); err != nil {
			return fmt.Errorf("flatKV WAL failed: %w", err)
		}
		return fmt.Errorf("flatKV WAL is closed")
	}
}

// serializerLoop turns dataToBeSerialized messages into dataToBeWritten messages and forwards every message to
// the writer in FIFO order. Runs on its own goroutine until close or a fatal error.
func (w *flatKVWalImpl) serializerLoop() {
	defer w.wg.Done()
	for {
		var msg any
		select {
		case <-w.ctx.Done():
			return
		case msg = <-w.serializerChan:
		}

		// A dataToBeSerialized becomes a dataToBeWritten; all other messages are forwarded unchanged.
		if req, ok := msg.(dataToBeSerialized); ok {
			payload, err := req.entry.Serialize()
			if err != nil {
				w.fail(fmt.Errorf("failed to serialize WAL entry: %w", err))
				return
			}
			msg = dataToBeWritten{
				record:      frameRecord(payload),
				blockNumber: req.entry.BlockNumber,
				endOfBlock:  req.entry.EndOfBlock,
			}
		}

		select {
		case w.writerChan <- msg:
		case <-w.ctx.Done():
			return
		}

		if _, ok := msg.(closeRequest); ok {
			// FIFO guarantees every prior write has been forwarded. Stop reading and forbid further
			// pushes so any racing/future schedule aborts instead of deadlocking.
			w.senderCancel()
			return
		}
	}
}

// writerLoop consumes forwarded messages, appending records to the mutable file and handling control messages.
// It owns all file bookkeeping and runs on its own goroutine until close or a fatal error.
func (w *flatKVWalImpl) writerLoop() {
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
			m.reply <- w.blockRange()
		case pruneRequest:
			if err := w.pruneSealedFiles(m.through); err != nil {
				w.fail(err)
				return
			}
		case iteratorStartRequest:
			m.reply <- w.startIterator(m.startBlock)
		case unpinRequest:
			w.releaseBlock(m.block)
		case closeRequest:
			_, err := w.mutableFile.seal()
			m.done <- err
			return
		}
	}
}

// appendRecord appends a record to the mutable file, updates bookkeeping, and rotates on block boundaries once
// the file exceeds the target size.
func (w *flatKVWalImpl) appendRecord(m dataToBeWritten) error {
	if err := w.mutableFile.writeRecord(m.record, m.blockNumber, m.endOfBlock); err != nil {
		return fmt.Errorf("failed to append record for block %d: %w", m.blockNumber, err)
	}
	walBytesWritten.Add(w.ctx, int64(len(m.record)))

	if m.endOfBlock {
		walBlocksWritten.Add(w.ctx, 1)
		if w.mutableFile.size >= uint64(w.config.TargetFileSize) {
			if err := w.rotate(); err != nil {
				return fmt.Errorf("failed to rotate after block %d: %w", m.blockNumber, err)
			}
		}
	}
	return nil
}

// rotate seals the current mutable file, records its bookkeeping, and opens a fresh mutable file. It is only
// called immediately after an end-of-block marker, so the mutable file ends on a block boundary.
func (w *flatKVWalImpl) rotate() error {
	index := w.mutableFile.index
	first := w.mutableFile.firstBlock
	last := w.mutableFile.lastCompleteBlock
	sealedName, err := w.mutableFile.seal()
	if err != nil {
		return fmt.Errorf("failed to seal WAL file during rotation: %w", err)
	}
	w.sealedFiles.PushBack(&sealedFileInfo{index: index, name: sealedName, firstBlock: first, lastBlock: last})
	walFilesSealed.Add(w.ctx, 1)

	mutable, err := newWalFile(w.config.Path, w.nextIndex)
	if err != nil {
		return fmt.Errorf("failed to open new mutable WAL file during rotation: %w", err)
	}
	w.mutableFile = mutable
	w.nextIndex++
	return nil
}

// pruneSealedFiles deletes sealed files whose highest block is below pruneThrough. Files are removed
// oldest-first (from the front of the deque) with a directory fsync after each removal, so a crash mid-prune
// leaves a contiguous suffix of files rather than a gap in the block sequence. The mutable file is never
// pruned.
//
// A live iterator holds a read lease at some block R and may still read every block from R onward, so no file
// whose range reaches R or higher may be removed. A file [first, last] is needed iff it overlaps [R, ∞), i.e.
// iff last >= R. Comparing the lowest live reservation against each file's last block (rather than testing
// whether the reservation falls inside a file's range) protects exactly the files an iterator can still open —
// even when the reservation lands in a gap between files or strictly inside a file's range. Because
// reservations never fall below the lowest stored block (see pinLowestReadableBlock), a file left below the
// lowest reservation is one the iterator has already moved past and can safely be dropped.
//
// Iteration stops at the first retained file: block ranges grow toward the back, so once a file is kept (by
// pruneThrough or by the lowest reservation) every later file is kept too.
func (w *flatKVWalImpl) pruneSealedFiles(pruneThrough uint64) error {
	// Reservations are mutated only on this (the writer) goroutine, so the lowest reservation is stable for the
	// duration of this prune and can be computed once.
	reservation, hasReservation := w.lowestReservation()
	for {
		front, ok := w.sealedFiles.TryPeekFront()
		if !ok || front.lastBlock >= pruneThrough {
			break
		}
		if hasReservation && front.lastBlock >= reservation {
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
// sealForIterator) so every complete block written so far lives in an immutable sealed file, then snapshots
// the sealed files in ascending block order, registers the read lease, and constructs the iterator (which
// launches its reader goroutine). Running here serializes construction with rotation, seal, and prune, so the
// snapshot is a consistent point-in-time view: every file the iterator reads is sealed and immutable, opened
// lazily by name and protected from pruning by the lease, so its contents cannot change underneath the reader.
func (w *flatKVWalImpl) startIterator(startBlock uint64) iteratorStartResponse {
	if err := w.sealForIterator(); err != nil {
		return iteratorStartResponse{err: fmt.Errorf("failed to seal mutable file before creating iterator: %w", err)}
	}

	files := make([]iteratorFile, 0, w.sealedFiles.Size())
	for _, info := range w.sealedFiles.Iterator() {
		files = append(files, iteratorFile{
			index:      info.index,
			name:       info.name,
			firstBlock: info.firstBlock,
			lastBlock:  info.lastBlock,
		})
	}

	pinned := w.pinLowestReadableBlock(startBlock)
	it := newWalIterator(w, startBlock, pinned, files, w.config.IteratorPrefetchSize)
	return iteratorStartResponse{iterator: it}
}

// sealForIterator seals the mutable file so a newly-created iterator sees a snapshot that cannot change
// underneath it: after this call every complete block lives in an immutable sealed file. Any in-progress
// (unended) block is carried forward into the fresh mutable file so no scheduled write is lost. It is a no-op
// when the mutable file holds no complete block — the iterator reads only sealed files and never yields an
// unended block, so the mutable file (and any in-progress block) is simply left in place.
func (w *flatKVWalImpl) sealForIterator() error {
	if !w.mutableFile.hasCompleteBlock {
		return nil
	}

	// Capture any in-progress block (records past the last end-of-block marker) before the seal truncates
	// it away, so it can be re-appended to the fresh mutable file. The write-ordering contract guarantees
	// these records all belong to a single block, namely the mutable file's last block.
	tail, err := w.mutableFile.readIncompleteTail()
	if err != nil {
		return fmt.Errorf("failed to capture in-progress block: %w", err)
	}
	tailBlock := w.mutableFile.lastBlock

	if err := w.rotate(); err != nil {
		return fmt.Errorf("failed to seal mutable file: %w", err)
	}

	if len(tail) > 0 {
		if err := w.mutableFile.appendIncompleteTail(tail, tailBlock); err != nil {
			return fmt.Errorf("failed to carry in-progress block forward: %w", err)
		}
	}
	return nil
}

// pinLowestReadableBlock records a read lease and returns the pinned block. An iterator reads blocks at or
// above startBlock but never below the oldest block actually stored, so the lease is clamped up to that: a
// stale low start must not pin files that no longer exist (or wedge pruning forever). Clamping to the oldest
// stored block also establishes the invariant pruneSealedFiles relies on: a reservation never falls below the
// lowest stored block, so a file entirely below the lowest reservation is one every iterator has moved past.
func (w *flatKVWalImpl) pinLowestReadableBlock(startBlock uint64) uint64 {
	pinned := startBlock
	if r := w.blockRange(); r.ok && r.start > pinned {
		pinned = r.start
	}
	w.blockRefs[pinned]++
	return pinned
}

// releaseBlock drops one reference to a leased block, forgetting it once the count reaches zero.
func (w *flatKVWalImpl) releaseBlock(block uint64) {
	if w.blockRefs[block] <= 1 {
		delete(w.blockRefs, block)
		return
	}
	w.blockRefs[block]--
}

// lowestReservation returns the smallest block number currently leased by a live iterator, and ok=false when no
// lease is held. A lease at block R means some iterator may still read blocks at or above R, so every sealed
// file whose range reaches R or higher must be retained by pruning.
func (w *flatKVWalImpl) lowestReservation() (uint64, bool) {
	var lowest uint64
	found := false
	for block := range w.blockRefs {
		if !found || block < lowest {
			lowest = block
			found = true
		}
	}
	return lowest, found
}

// blockRange reports the range of complete blocks across all files. Complete blocks live in the sealed files
// (all complete) and in the mutable file up to its last end-of-block marker. Owned by the writer goroutine.
func (w *flatKVWalImpl) blockRange() storedRange {
	var r storedRange

	// The highest complete block is in the mutable file if it has one, otherwise in the newest sealed file.
	if w.mutableFile.hasCompleteBlock {
		r = storedRange{ok: true, end: w.mutableFile.lastCompleteBlock}
	} else if back, ok := w.sealedFiles.TryPeekBack(); ok {
		r = storedRange{ok: true, end: back.lastBlock}
	} else {
		return storedRange{} // nothing complete stored yet
	}

	// The lowest stored block is in the oldest sealed file if any, otherwise in the mutable file.
	if front, ok := w.sealedFiles.TryPeekFront(); ok {
		r.start = front.firstBlock
	} else {
		r.start = w.mutableFile.firstBlock
	}
	return r
}

// fail records the first fatal background error and triggers shutdown of the pipeline.
func (w *flatKVWalImpl) fail(err error) {
	w.asyncErr.CompareAndSwap(nil, &err)
	w.cancel()
	logger.Error("flatKV WAL encountered a fatal error", "err", err)
}

// asyncError returns the first fatal background error, or nil if none occurred.
func (w *flatKVWalImpl) asyncError() error {
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

// rollbackDirectory drops all data beyond rollbackThrough from the sealed files. Assumes orphans are already
// sealed. Files are processed highest-index-first: the files entirely beyond the rollback point (a suffix of
// the index sequence) are removed one at a time, each removal made durable before the next, and finally the
// single file straddling the rollback point is truncated. This ordering guarantees that a crash mid-rollback
// always leaves a contiguous prefix of files — never a gap that scanSealedFiles would reject — mirroring the
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
		names[parsed.index] = entry.Name()
	}
	sort.Slice(sealed, func(i int, j int) bool { return sealed[i].index > sealed[j].index })

	for _, parsed := range sealed {
		if parsed.lastBlock <= rollbackThrough {
			// This file lies entirely at or below the rollback point; so does every lower-indexed file. Done.
			break
		}
		if parsed.firstBlock > rollbackThrough {
			// Entirely beyond the rollback point: remove the whole file, durably, before the next-lower one.
			if err := removeAndSyncDir(directory, names[parsed.index]); err != nil {
				return fmt.Errorf("failed to roll back %s: %w", names[parsed.index], err)
			}
			continue
		}
		// Straddles the rollback point: truncate away the blocks beyond it. This is the last file to process.
		if err := rollbackStraddlingFile(directory, names[parsed.index], rollbackThrough); err != nil {
			return fmt.Errorf("failed to roll back %s: %w", names[parsed.index], err)
		}
	}
	return nil
}

// scanSealedFiles loads the sealed files in a directory into an ascending-order deque and returns the index to
// assign the next mutable file (one past the highest sealed index, or 0 when there are none). File indices
// must be contiguous: a gap means a sealed file went missing, which is unrecoverable corruption, so this fails
// with an informative error rather than silently leaving a hole in the block sequence.
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
		names[p.index] = entry.Name()
	}
	sort.Slice(parsed, func(i int, j int) bool { return parsed[i].index < parsed[j].index })

	sealedFiles := util.NewRandomAccessDeque[*sealedFileInfo](uint64(len(parsed)))
	var nextIndex uint64
	for i, p := range parsed {
		if i > 0 && p.index != parsed[i-1].index+1 {
			return nil, 0, fmt.Errorf(
				"WAL is corrupt: sealed file indices are not contiguous (gap between %d and %d)",
				parsed[i-1].index, p.index)
		}
		sealedFiles.PushBack(&sealedFileInfo{
			index: p.index, name: names[p.index], firstBlock: p.firstBlock, lastBlock: p.lastBlock})
		nextIndex = p.index + 1
	}
	return sealedFiles, nextIndex, nil
}
