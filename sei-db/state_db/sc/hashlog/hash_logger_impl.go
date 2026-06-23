package hashlog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/seilog"
)

var _ HashLogger = (*hashLoggerImpl)(nil)

var logger = seilog.NewLogger("db", "state-db", "sc", "hashlog")

// The kind of message sent to the control loop.
type controlMsgKind int

const (
	ctrlHashReport  controlMsgKind = iota // a caller-reported hash for a block
	ctrlDiffRequest                       // a change set to be diff-hashed on the hasher thread
	ctrlRollback                          // an explicit rollback signal
	ctrlClose                             // a graceful-shutdown signal (sent by Close)
)

// Data flow:
//
// The control loop gathers and colates hash data. When it gets the diff for a block, it farms the work of hashing
// that diff to the hasher goroutine. When it collects enough data to write to the hash log, it offloads the write
// to the writer goroutine.
//
//	ReportHash ─────────┐                     ┌──────writerChan─────▶ writer
//	ReportDiff ─────────┤                     │
//	SignalRollback ─────┴───controlChan──▶ controlLoop ───hashChan──▶ hasher
//	                                          ▲                         │
//	                                          └──────hashResultChan─────┘

// A message destined for the control loop. All caller entry points (ReportHash, ReportDiff, SignalRollback)
// and Close funnel through controlChan as a single ordered stream, so the control loop always knows which
// blocks have a diff hash in flight and can order rollbacks and shutdown against the reports around them.
type controlMessage struct {
	kind        controlMsgKind
	blockNumber uint64
	hashType    string                  // ctrlHashReport: the type being reported
	hash        []byte                  // ctrlHashReport: the reported hash (may be nil)
	cs          []*proto.NamedChangeSet // ctrlDiffRequest: the change set to hash
	done        chan struct{}           // ctrlRollback / ctrlClose: closed once the flush/drain completes
}

// A change set dispatched from the control loop to the hasher.
type hashWork struct {
	blockNumber uint64
	cs          []*proto.NamedChangeSet
}

// A computed diff hash returned from the hasher to the control loop.
type hashResult struct {
	blockNumber uint64
	hash        []byte
}

// A message destined for the writer: either a block to write or a rollback barrier. Exactly one field is set.
type writerMessage struct {
	// A block to append to the current file.
	log *HashLog
	// A rollback barrier: flush the current file to disk (making prior writes durable) and close this channel.
	rollbackAck chan struct{}
}

// Bookkeeping for a sealed hash log file (owned by the writer goroutine).
type sealedFileInfo struct {
	name       string
	firstBlock uint64
	lastBlock  uint64
	size       uint64
}

// A standard hash logger implementation.
type hashLoggerImpl struct {
	// Immutable configuration captured at construction.
	directory   string
	version     string // sanitized
	hashTypes   []string
	hashTypeSet map[string]struct{}

	// The name of the logger-owned diff column. Meaningful only when diff hashing is enabled.
	diffHashType string
	// When true, diff hashing is disabled: no hasher thread, ReportDiff is a no-op, and no diff column is
	// recorded or awaited.
	diffHashingDisabled bool

	targetFileSize    uint64
	blocksToRetain    uint64
	maxDiskSize       uint64
	maxBufferedBlocks uint64

	// For sending work to the control loop (the hub for all caller entry points).
	controlChan chan controlMessage

	// For sending work to the writer thread.
	writerChan chan writerMessage

	// For dispatching diff work from the control loop to the hasher, and for the hasher to return results.
	// Both nil if diff hashing is disabled.
	hashChan       chan hashWork
	hashResultChan chan hashResult

	// ctx is the hard-stop context: the hasher, writer, and the control loop's downstream sends watch it, and it
	// is cancelled by fail() on error and by Close() once everything has drained. senderCtx is a child of ctx
	// that the controlChan producers (ReportHash/ReportDiff/SignalRollback) watch; the control loop cancels it
	// once it stops reading controlChan, so an in-flight or future push aborts rather than deadlocking. Because
	// controlChan is never closed, those pushes can never panic on a closed channel.
	ctx          context.Context
	cancel       context.CancelFunc
	senderCtx    context.Context
	senderCancel context.CancelFunc
	wg           sync.WaitGroup
	closeOnce    sync.Once
	closed       atomic.Bool
	asyncErr     atomic.Pointer[error]

	// The following fields are owned exclusively by the writer goroutine.

	// The current mutable file accepting writes.
	mutableFile *hashLogFile

	// Bookkeeping for sealed files, keyed by file index.
	sealedFiles map[uint64]*sealedFileInfo

	// The number of bytes currently used by sealed log files.
	currentDiskSpaceUsed uint64

	// The index of the oldest hash log file currently tracked.
	lowestLogFileIndex uint64

	// The index of the mutable hash log file (i.e. the latest one).
	mutableLogFileIndex uint64

	// The highest block number written to disk so far (used for block-count retention).
	latestBlock uint64

	// The following fields are the control loop's private bookkeeping, owned exclusively by the control loop
	// goroutine, so they need no synchronization.

	// Blocks being assembled, keyed by block number.
	pendingBlocks map[uint64]*HashLog

	// Blocks with a diff dispatched to the hasher whose result has not yet returned. Such a block is never
	// force-flushed by the overflow path: its diff is on the way.
	blocksWithPendingHashes map[uint64]struct{}

	// The single hash job that has been read off controlChan but not yet enqueued into hashChan, because the
	// hasher fell behind and hashChan's buffer is full. hashChan's buffer is the real in-flight queue (many
	// jobs); this is the one job that couldn't fit, and it is the backpressure boundary: while it is set the
	// control loop stops reading new control messages (so ReportDiff blocks upstream), but it keeps draining
	// results, which keeps the loop⇄hasher pair deadlock-free.
	workAwaitingDispatch *hashWork

	// The highest block number written to disk. While valid, reports for blocks at or below it are discarded
	// (they are already flushed); SignalRollback clears this so re-executed blocks are accepted again.
	flushedHighWater uint64
	flushedValid     bool
}

// NewHashLogger creates a HashLogger that writes to config.Path.
func NewHashLogger(config *HashLoggerConfig) (HashLogger, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid hash logger config: %w", err)
	}

	if err := util.EnsureDirectoryExists(config.Path, true); err != nil {
		return nil, fmt.Errorf("failed to ensure hash log directory %s: %w", config.Path, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	senderCtx, senderCancel := context.WithCancel(ctx)

	// The recorded columns are the caller-reported types, with the logger-owned diff column prepended when
	// diff hashing is enabled.
	var hashTypes []string
	if !config.DisableDiffHashing {
		hashTypes = append(hashTypes, config.DiffHashType)
	}
	hashTypes = append(hashTypes, config.HashTypes...)
	hashTypeSet := make(map[string]struct{}, len(hashTypes))
	for _, hashType := range hashTypes {
		hashTypeSet[hashType] = struct{}{}
	}

	h := &hashLoggerImpl{
		directory:               config.Path,
		version:                 sanitizeVersion(config.Version),
		hashTypes:               hashTypes,
		hashTypeSet:             hashTypeSet,
		diffHashType:            config.DiffHashType,
		diffHashingDisabled:     config.DisableDiffHashing,
		targetFileSize:          uint64(config.TargetFileSize),
		blocksToRetain:          uint64(config.BlocksToRetain),
		maxDiskSize:             uint64(config.MaxDiskSize),
		maxBufferedBlocks:       uint64(config.MaxBufferedBlocks),
		controlChan:             make(chan controlMessage, config.ControlBufferSize),
		writerChan:              make(chan writerMessage, config.WriteBufferSize),
		ctx:                     ctx,
		cancel:                  cancel,
		senderCtx:               senderCtx,
		senderCancel:            senderCancel,
		sealedFiles:             make(map[uint64]*sealedFileInfo),
		pendingBlocks:           make(map[uint64]*HashLog),
		blocksWithPendingHashes: make(map[uint64]struct{}),
	}
	if !h.diffHashingDisabled {
		h.hashChan = make(chan hashWork, config.HashBufferSize)
		h.hashResultChan = make(chan hashResult, config.HashBufferSize)
	}

	if err := h.scanDirectory(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to scan hash log directory: %w", err)
	}

	mutableFile, err := newHashLogFile(h.directory, h.mutableLogFileIndex, h.version, h.hashTypes)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create mutable hash log file: %w", err)
	}
	h.mutableFile = mutableFile

	if !h.diffHashingDisabled {
		h.wg.Add(1)
		go h.hasher()
	}
	h.wg.Add(1)
	go h.controlLoop()
	h.wg.Add(1)
	go h.writer()

	return h, nil
}

// Scan the directory: seal any orphaned ".hlog.u" files left by a crash, then index the sealed files so the
// writer can resume rotation and GC where the previous session left off.
func (h *hashLoggerImpl) scanDirectory() error {
	entries, err := os.ReadDir(h.directory)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", h.directory, err)
	}

	// First pass: seal orphaned unsealed files.
	for _, entry := range entries {
		isHashLog, isSealed := isHashLogFileName(entry.Name())
		if isHashLog && !isSealed {
			if err := sealHashLog(filepath.Join(h.directory, entry.Name())); err != nil {
				return fmt.Errorf("failed to seal orphaned file %s: %w", entry.Name(), err)
			}
		}
	}

	// Re-read the directory now that orphans have been sealed (or removed).
	entries, err = os.ReadDir(h.directory)
	if err != nil {
		return fmt.Errorf("failed to re-read directory %s: %w", h.directory, err)
	}

	var maxIndex uint64
	lowestIndex := ^uint64(0)
	hasSealedFiles := false
	for _, entry := range entries {
		parsed, ok := parseFileName(entry.Name())
		if !ok || !parsed.sealed {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", entry.Name(), err)
		}
		size := uint64(info.Size())
		h.sealedFiles[parsed.index] = &sealedFileInfo{
			name:       entry.Name(),
			firstBlock: parsed.firstBlock,
			lastBlock:  parsed.lastBlock,
			size:       size,
		}
		h.currentDiskSpaceUsed += size
		if parsed.index > maxIndex {
			maxIndex = parsed.index
		}
		if parsed.index < lowestIndex {
			lowestIndex = parsed.index
		}
		if parsed.lastBlock > h.latestBlock {
			h.latestBlock = parsed.lastBlock
		}
		hasSealedFiles = true
	}

	if hasSealedFiles {
		h.lowestLogFileIndex = lowestIndex
		h.mutableLogFileIndex = maxIndex + 1
	} else {
		h.lowestLogFileIndex = 0
		h.mutableLogFileIndex = 0
	}
	return nil
}

func (h *hashLoggerImpl) ReportDiff(blockNumber uint64, cs []*proto.NamedChangeSet) {
	// Calling Report* after Close() violates the contract; fail fast (no-op) rather than risk a send on a
	// closed channel.
	if h.closed.Load() {
		return
	}
	if h.diffHashingDisabled {
		return
	}
	// A nil change set means the caller is opting out of diff hashing for this block: record a nil diff hash
	// (which still completes the block) without bothering the hasher thread. An empty (non-nil) change set is a
	// legitimate no-change block and falls through to be hashed normally (yielding the hash of the empty diff).
	if cs == nil {
		h.sendControl(
			controlMessage{kind: ctrlHashReport, blockNumber: blockNumber, hashType: h.diffHashType, hash: nil})
		return
	}
	// Blocking send to the control loop, which dispatches the change set to the hasher. The diff is never
	// dropped, so the recorded diff hash always reflects the change set that was reported.
	h.sendControl(controlMessage{kind: ctrlDiffRequest, blockNumber: blockNumber, cs: cs})
}

func (h *hashLoggerImpl) ReportHash(blockNumber uint64, hashType string, hash []byte) error {
	// Calling Report* after Close() violates the contract; fail fast rather than risk a send on a closed
	// channel.
	if h.closed.Load() {
		return fmt.Errorf("hash logger is closed")
	}
	// The diff column is logger-owned: it is computed internally from ReportDiff, not supplied by the caller.
	// Reject it here so a caller can never clobber (or race) the computed diff hash via ReportHash.
	if !h.diffHashingDisabled && hashType == h.diffHashType {
		return fmt.Errorf("hash type %q is reserved for the logger-computed diff; use ReportDiff", hashType)
	}
	if _, ok := h.hashTypeSet[hashType]; !ok {
		return fmt.Errorf("unknown hash type %q", hashType)
	}
	// Blocking send to the control loop, which normally drains controlChan quickly; it can backpressure only
	// if the downstream writer is itself stalled on a slow disk.
	h.sendControl(controlMessage{kind: ctrlHashReport, blockNumber: blockNumber, hashType: hashType, hash: hash})
	return nil
}

// SignalRollback flushes all buffered blocks and resets the control loop's already-flushed tracking, so that
// re-executed blocks (whose numbers no longer advance) are logged rather than discarded. It blocks until the
// flush has completed, guaranteeing the control loop holds no pre-rollback state when it returns.
func (h *hashLoggerImpl) SignalRollback() error {
	if h.closed.Load() {
		return fmt.Errorf("hash logger is closed")
	}
	done := make(chan struct{})
	select {
	case h.controlChan <- controlMessage{kind: ctrlRollback, done: done}:
	case <-h.senderCtx.Done():
		return fmt.Errorf("hash logger is shutting down")
	}
	// Wait for the control loop to finish the flush so the rollback is durable before we return.
	select {
	case <-done:
		return nil
	case <-h.senderCtx.Done():
		return fmt.Errorf("hash logger is shutting down")
	}
}

func (h *hashLoggerImpl) Close() error {
	h.closeOnce.Do(func() {
		// Reject further Report*/SignalRollback calls (best effort; senderCtx is the real backstop).
		h.closed.Store(true)
		// controlChan is never closed. Instead we send a ctrlClose sentinel; FIFO ordering guarantees the
		// control loop processes every prior report before it, then drains, cancels senderCtx (so any racing or
		// future push aborts instead of deadlocking), and closes done. If an async error already cancelled ctx,
		// the control loop is gone, so skip the handshake.
		done := make(chan struct{})
		select {
		case h.controlChan <- controlMessage{kind: ctrlClose, done: done}:
			select {
			case <-done:
			case <-h.ctx.Done():
			}
		case <-h.ctx.Done():
		}
		h.wg.Wait()
		// Release the context now that every goroutine has exited.
		h.cancel()
	})
	if err := h.asyncErr.Load(); err != nil {
		return *err
	}
	return nil
}

// sendControl forwards a message to the control loop, giving up if the logger is shutting down. controlChan is
// never closed, so this can never panic; senderCtx (cancelled by the control loop once it stops reading, and by
// a hard error) unblocks a send that would otherwise wait on a control loop that is gone.
func (h *hashLoggerImpl) sendControl(msg controlMessage) {
	select {
	case h.controlChan <- msg:
	case <-h.senderCtx.Done():
	}
}

func (h *hashLoggerImpl) hasher() {
	defer h.wg.Done()
	for {
		select {
		case <-h.ctx.Done():
			// Hard stop (error path); abandon in-flight diffs.
			return
		case work, ok := <-h.hashChan:
			if !ok {
				// The control loop closed hashChan after draining all in-flight diffs; nothing left to do.
				return
			}
			result := hashResult{blockNumber: work.blockNumber, hash: hashDiff(work.cs)}
			select {
			case h.hashResultChan <- result:
			case <-h.ctx.Done():
				return
			}
		}
	}
}

func (h *hashLoggerImpl) controlLoop() {
	defer h.wg.Done()

	for {
		if h.workAwaitingDispatch == nil {
			select {
			case <-h.ctx.Done():
				return
			case msg := <-h.controlChan:
				if msg.kind == ctrlClose {
					// FIFO guarantees every prior report has been handled. Flush, then forbid further pushes
					// (senderCancel) so any racing/future send aborts instead of deadlocking, and ack Close.
					h.gracefulDrain()
					h.senderCancel()
					close(msg.done)
					return
				}
				h.handleControlMessage(msg)
			case res := <-h.hashResultChan: // nil channel when diff hashing is disabled; case never fires
				h.applyDiffResult(res)
			}
		} else {
			// A diff is waiting to be dispatched: offer it to the hasher while still draining results, so the
			// control loop and hasher can never deadlock sending to each other.
			select {
			case <-h.ctx.Done():
				return
			case res := <-h.hashResultChan:
				h.applyDiffResult(res)
			case h.hashChan <- *h.workAwaitingDispatch:
				h.workAwaitingDispatch = nil
			}
		}
		h.flushProgress()
	}
}

// handleControlMessage routes a single control message to the appropriate handler.
func (h *hashLoggerImpl) handleControlMessage(msg controlMessage) {
	switch msg.kind {
	case ctrlHashReport:
		h.handleHashReport(msg.blockNumber, msg.hashType, msg.hash)
	case ctrlDiffRequest:
		h.handleDiffRequest(msg.blockNumber, msg.cs)
	case ctrlRollback:
		h.handleRollback(msg.done)
	}
}

// handleHashReport records a caller-reported hash, discarding it if the block has already been flushed.
func (h *hashLoggerImpl) handleHashReport(blockNumber uint64, hashType string, hash []byte) {
	if h.flushedValid && blockNumber <= h.flushedHighWater {
		return // already on disk: a duplicate/late report, or a re-execution without SignalRollback
	}
	h.ensurePending(blockNumber).Hashes[hashType] = hash
}

// handleDiffRequest records that a block is awaiting a diff hash and holds the work for dispatch to the hasher.
func (h *hashLoggerImpl) handleDiffRequest(blockNumber uint64, cs []*proto.NamedChangeSet) {
	if h.flushedValid && blockNumber <= h.flushedHighWater {
		return // already on disk; discard
	}
	// Create the pending entry now so minPendingKey accounts for diff-only blocks and the overflow path can see
	// that the oldest block is awaiting a diff.
	h.ensurePending(blockNumber)
	h.blocksWithPendingHashes[blockNumber] = struct{}{}
	h.workAwaitingDispatch = &hashWork{blockNumber: blockNumber, cs: cs}
}

// applyDiffResult records a computed diff hash and clears the block's pending-diff marker.
func (h *hashLoggerImpl) applyDiffResult(res hashResult) {
	delete(h.blocksWithPendingHashes, res.blockNumber)
	if h.flushedValid && res.blockNumber <= h.flushedHighWater {
		return // the block was already flushed (e.g. across a rollback); discard the stale diff
	}
	h.ensurePending(res.blockNumber).Hashes[h.diffHashType] = res.hash
}

// handleRollback flushes all buffered work to disk, then resets the already-flushed tracking so re-executed
// blocks are accepted. It hands a barrier to the writer (which makes the flush durable and then releases the
// SignalRollback caller via done) and resets ordering so subsequent reports are the new timeline.
// workAwaitingDispatch is always nil here: rollback is only received in the workAwaitingDispatch==nil branch.
func (h *hashLoggerImpl) handleRollback(done chan struct{}) {
	h.drainInFlightDiffs()
	for len(h.pendingBlocks) > 0 {
		h.emit(minPendingKey(h.pendingBlocks))
	}
	// The barrier is ordered after every emitted block, so the writer makes them durable before closing done.
	h.blockingSendToWriter(writerMessage{rollbackAck: done})
	h.flushedValid = false
}

// ensurePending returns the pending HashLog for a block, creating an empty one if needed.
func (h *hashLoggerImpl) ensurePending(blockNumber uint64) *HashLog {
	log, ok := h.pendingBlocks[blockNumber]
	if !ok {
		log = &HashLog{BlockNumber: blockNumber, Hashes: make(map[string][]byte, len(h.hashTypes))}
		h.pendingBlocks[blockNumber] = log
	}
	return log
}

// flushProgress emits every block it can: first the contiguous prefix of complete blocks, then — while the
// buffer still exceeds maxBufferedBlocks — the oldest incomplete block, to bound memory. It never force-flushes
// a block awaiting an in-flight diff (its diff is on the way), even if that leaves the buffer over the bound.
func (h *hashLoggerImpl) flushProgress() {
	for {
		h.drainComplete()
		if uint64(len(h.pendingBlocks)) <= h.maxBufferedBlocks {
			return
		}
		oldest := minPendingKey(h.pendingBlocks)
		if _, awaitingDiff := h.blocksWithPendingHashes[oldest]; awaitingDiff {
			return // don't force-flush a block whose diff is still being computed
		}
		h.emit(oldest)
	}
}

// drainComplete emits the contiguous prefix of complete blocks (oldest first), stopping at the first incomplete
// block so that blocks are always written in increasing order.
func (h *hashLoggerImpl) drainComplete() {
	for len(h.pendingBlocks) > 0 {
		oldest := minPendingKey(h.pendingBlocks)
		if len(h.pendingBlocks[oldest].Hashes) < len(h.hashTypes) {
			break
		}
		h.emit(oldest)
	}
}

// drainInFlightDiffs blocks until every dispatched diff has returned, applying each result, so no diff result
// can arrive after the buffer has been flushed. Used by rollback and shutdown.
func (h *hashLoggerImpl) drainInFlightDiffs() {
	for len(h.blocksWithPendingHashes) > 0 {
		select {
		case res := <-h.hashResultChan:
			h.applyDiffResult(res)
		case <-h.ctx.Done():
			return
		}
	}
}

// emit writes a single block to the writer and records it as flushed.
func (h *hashLoggerImpl) emit(blockNumber uint64) {
	log := h.pendingBlocks[blockNumber]
	delete(h.pendingBlocks, blockNumber)
	delete(h.blocksWithPendingHashes, blockNumber)
	h.blockingSendToWriter(writerMessage{log: log})
	h.flushedHighWater = blockNumber
	h.flushedValid = true
}

// gracefulDrain flushes all remaining work on a clean shutdown, then closes the downstream channels so the
// hasher and writer drain and exit.
func (h *hashLoggerImpl) gracefulDrain() {
	h.drainInFlightDiffs()
	for len(h.pendingBlocks) > 0 {
		h.emit(minPendingKey(h.pendingBlocks))
	}
	if !h.diffHashingDisabled {
		close(h.hashChan)
	}
	close(h.writerChan)
}

// blockingSendToWriter delivers a message to the writer, giving up only if the logger is shutting down. A slow
// writer (slow disk) therefore backpressures the control loop, which backpressures the upstream channels and
// ultimately the caller — nothing is dropped.
func (h *hashLoggerImpl) blockingSendToWriter(msg writerMessage) {
	select {
	case h.writerChan <- msg:
	case <-h.ctx.Done():
	}
}

func (h *hashLoggerImpl) writer() {
	defer h.wg.Done()
	for {
		select {
		case <-h.ctx.Done():
			// Hard stop (error path): leave the mutable file unsealed; it is recovered on next startup.
			return
		case msg, ok := <-h.writerChan:
			if !ok {
				// Graceful shutdown: seal the final file so a clean shutdown leaves no ".hlog.u" behind.
				if err := h.sealMutableAndGC(); err != nil {
					h.fail(err)
				}
				return
			}
			if msg.rollbackAck != nil {
				// Rollback barrier: flush the current file to disk so prior writes are durable, then release
				// the waiting SignalRollback caller. The new timeline auto-rotates to a fresh file when its
				// first (regressing) block arrives.
				if err := h.mutableFile.flush(); err != nil {
					h.fail(err)
					return
				}
				close(msg.rollbackAck)
				continue
			}
			if err := h.handleWrite(msg.log); err != nil {
				h.fail(err)
				return
			}
		}
	}
}

func (h *hashLoggerImpl) handleWrite(log *HashLog) error {
	// A block number that doesn't advance indicates a rollback (re-execution of an earlier height). Seal the
	// current file and start a new one so every file's block numbers stay monotonic. The writer detects this
	// itself from the block numbers it receives, rather than relying on a separate control signal.
	if h.mutableFile.hasBlocks && log.BlockNumber <= h.mutableFile.lastBlockIndex {
		if err := h.rotate(); err != nil {
			return err
		}
	}
	if err := h.mutableFile.write(log); err != nil {
		return err
	}
	if log.BlockNumber > h.latestBlock {
		h.latestBlock = log.BlockNumber
	}
	if h.mutableFile.size >= h.targetFileSize {
		if err := h.rotate(); err != nil {
			return err
		}
	}
	return nil
}

// rotate seals the current mutable file, records its bookkeeping, opens a fresh mutable file, and runs GC.
func (h *hashLoggerImpl) rotate() error {
	if err := h.recordSealedFile(); err != nil {
		return err
	}
	h.mutableLogFileIndex++
	newFile, err := newHashLogFile(h.directory, h.mutableLogFileIndex, h.version, h.hashTypes)
	if err != nil {
		return fmt.Errorf("failed to open new mutable hash log file: %w", err)
	}
	h.mutableFile = newFile
	h.runGC()
	return nil
}

func (h *hashLoggerImpl) sealMutableAndGC() error {
	if err := h.recordSealedFile(); err != nil {
		return err
	}
	h.runGC()
	return nil
}

// recordSealedFile seals the current mutable file and, if it held any blocks, adds it to the sealed-file
// bookkeeping. An empty file is removed by close() and leaves no bookkeeping behind.
func (h *hashLoggerImpl) recordSealedFile() error {
	hadBlocks := h.mutableFile.hasBlocks
	idx := h.mutableFile.index
	first := h.mutableFile.firstBlockIndex
	last := h.mutableFile.lastBlockIndex
	size := h.mutableFile.size

	if err := h.mutableFile.close(); err != nil {
		return fmt.Errorf("failed to seal hash log file: %w", err)
	}
	if !hadBlocks {
		return nil
	}
	h.sealedFiles[idx] = &sealedFileInfo{
		name:       sealedFileName(idx, first, last, h.version),
		firstBlock: first,
		lastBlock:  last,
		size:       size,
	}
	h.currentDiskSpaceUsed += size
	return nil
}

// runGC deletes the oldest sealed files while either the block-count retention window or the disk-size cap is
// exceeded. The mutable file is never considered.
func (h *hashLoggerImpl) runGC() {
	for h.lowestLogFileIndex < h.mutableLogFileIndex {
		idx := h.lowestLogFileIndex
		info, ok := h.sealedFiles[idx]
		if !ok {
			// No sealed file at this index (e.g. it was empty and removed); advance past it.
			h.lowestLogFileIndex++
			continue
		}

		overBlockRetention := h.latestBlock > h.blocksToRetain &&
			info.lastBlock < h.latestBlock-h.blocksToRetain
		overSizeCap := h.currentDiskSpaceUsed > h.maxDiskSize
		if !overBlockRetention && !overSizeCap {
			break
		}

		if err := h.deleteSealedFile(idx, info); err != nil {
			logger.Error("failed to delete sealed hash log file during GC", "index", idx, "error", err)
			break
		}
		h.lowestLogFileIndex++
	}
}

func (h *hashLoggerImpl) deleteSealedFile(idx uint64, info *sealedFileInfo) error {
	path := filepath.Join(h.directory, info.name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove sealed file %s: %w", path, err)
	}
	h.currentDiskSpaceUsed -= info.size
	delete(h.sealedFiles, idx)
	return nil
}

func (h *hashLoggerImpl) fail(err error) {
	if h.asyncErr.CompareAndSwap(nil, &err) {
		logger.Error("hash logger encountered an unrecoverable error; shutting down", "error", err)
	}
	h.cancel()
}

// minPendingKey returns the smallest block number in pending. pending must be non-empty.
func minPendingKey(pending map[uint64]*HashLog) uint64 {
	var min uint64
	first := true
	for blockNumber := range pending {
		if first || blockNumber < min {
			min = blockNumber
			first = false
		}
	}
	return min
}

// sanitizeVersion replaces every character outside [A-Za-z0-9._] (notably spaces and "-") with "_" so the
// version can be embedded unambiguously in a hash log file name.
func sanitizeVersion(version string) string {
	var builder strings.Builder
	builder.Grow(len(version))
	for _, r := range version {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return builder.String()
}
