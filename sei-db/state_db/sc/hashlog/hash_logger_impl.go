package hashlog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/seilog"
)

var _ HashLogger = (*hashLoggerImpl)(nil)

var logger = seilog.NewLogger("db", "state-db", "sc", "hashlog")

// The kind of message sent to the control loop.
type controlMsgKind int

const (
	ctrlHashReport       controlMsgKind = iota // a caller-reported hash for a block
	ctrlChangesetRequest                       // a changeset to be hashed on the hasher thread
	ctrlColumnChange                           // add or remove a hash column (sent by Register/UnregisterHashType)
	ctrlClose                                  // a graceful-shutdown signal (sent by Close)
)

// Data flow:
//
// The control loop gathers and collates hash data. When it gets the changeset for a block, it farms the work of
// hashing that changeset to the hasher goroutine. When it collects enough data to write to the hash log, it
// offloads the write to the writer goroutine.
//
//	                                            ┌─────writerChan────▶ writer
//	ReportHash      ──┐                         │
//	ReportChangeset ──┴──controlChan──▶ controlLoop ──hashChan──▶ hasher
//	                                            ▲                   │
//	                                            └──hashResultChan───┘

// A message destined for the control loop. The caller entry points (ReportHash, ReportChangeset) and Close funnel
// through controlChan as a single ordered stream, so the control loop always knows which blocks have a changeset
// hash in flight and can order shutdown against the reports around it.
type controlMessage struct {
	kind        controlMsgKind
	blockNumber uint64
	hashType    string                  // ctrlHashReport / ctrlColumnChange: the type being reported/changed
	hash        []byte                  // ctrlHashReport: the reported hash (may be nil)
	cs          []*proto.NamedChangeSet // ctrlChangesetRequest: the change set to hash
	add         bool                    // ctrlColumnChange: true to add the column, false to remove it
	done        chan struct{}           // ctrlColumnChange / ctrlClose: closed once the loop has applied the message
}

// A change set dispatched from the control loop to the hasher.
type hashWork struct {
	blockNumber uint64
	cs          []*proto.NamedChangeSet
}

// A computed changeset hash returned from the hasher to the control loop.
type hashResult struct {
	blockNumber uint64
	hash        []byte
}

// A message destined for the writer: either a block to append to the current file, or (when rotate is
// true) a directive to seal the current file and open a fresh one with the given columns. The control
// loop sends complete blocks ahead of a rotate so they land in the file whose header matches their
// columns; messages are processed in FIFO order so blocks before a column change go to the old file and
// blocks after go to the new one.
type writerMessage struct {
	log       *HashLog
	rotate    bool
	hashTypes []string // rotate: the column set (header) for the new file
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
	// The directory all hash log files are written to.
	directory string

	// The software version embedded in each file name (sanitized to be filename-safe at construction).
	version string

	// The ordered set of hash columns recorded per block; the changeset column is prepended when changeset hashing is
	// enabled. Mutated only by the control loop (handling a ctrlColumnChange), so the loop reads len(hashTypes) for
	// block completion without synchronization. Register/UnregisterHashType change it through that message.
	hashTypes []string

	// The membership set over hashTypes, for O(1) validation of caller-supplied hash types in ReportHash. Written
	// only by the control loop (handling ctrlColumnChange) and read by the caller in Register/Unregister/ReportHash.
	// These callers are serialized (Register/Unregister block on the loop's ack via the done channel, establishing
	// happens-before), so the read is race-free as long as callers do not invoke the API concurrently.
	hashTypeSet map[string]struct{}

	// When true, changeset hashing is disabled: no hasher thread, ReportChangeset is a no-op, and no changeset column is
	// recorded or awaited.
	changesetHashingDisabled bool

	// The size a mutable file may reach before it is sealed and a fresh one is opened.
	targetFileSize uint64

	// The number of most-recent blocks to keep on disk; sealed files older than this window are garbage collected.
	blocksToRetain uint64

	// A backstop cap on the total size of sealed files; the oldest are garbage collected to stay under it, even
	// if that retains fewer than blocksToRetain blocks.
	maxDiskSize uint64

	// The most blocks the control loop will buffer before force-flushing the oldest incomplete block to bound
	// memory.
	maxBufferedBlocks uint64

	// For sending work to the control loop (the hub for all caller entry points).
	controlChan chan controlMessage

	// For sending work to the writer thread.
	writerChan chan writerMessage

	// For dispatching changeset work from the control loop to the hasher. Nil when changeset hashing is disabled.
	hashChan chan hashWork

	// For the hasher to return computed changeset hashes to the control loop. Nil when changeset hashing is disabled.
	hashResultChan chan hashResult

	// The hard-stop context that the hasher, writer, and the control loop's downstream sends all watch. Cancelled
	// by fail() on a fatal error and by Close() once everything has drained.
	ctx context.Context

	// Cancels ctx.
	cancel context.CancelFunc

	// A child of ctx that the controlChan producers (ReportHash/ReportChangeset) watch. The control loop cancels it
	// once it stops reading controlChan, so an in-flight or future push aborts rather than deadlocking. Because
	// controlChan is never closed, those pushes can never panic on a closed channel.
	senderCtx context.Context

	// Cancels senderCtx.
	senderCancel context.CancelFunc

	// Tracks the hasher, writer, and control-loop goroutines so Close() can wait for them to exit.
	wg sync.WaitGroup

	// Guarantees the Close() shutdown sequence runs at most once.
	closeOnce sync.Once

	// Set by Close() so subsequent Report* calls fail fast (best effort; senderCtx is the real backstop).
	closed atomic.Bool

	// The first unrecoverable background-goroutine error, surfaced to the caller by Close().
	asyncErr atomic.Pointer[error]

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

	// Blocks with a changeset dispatched to the hasher whose result has not yet returned. Such a block is never
	// force-flushed by the overflow path: its changeset is on the way.
	blocksWithPendingHashes map[uint64]struct{}

	// The single hash job that has been read off controlChan but not yet enqueued into hashChan, because the
	// hasher fell behind and hashChan's buffer is full. hashChan's buffer is the real in-flight queue (many
	// jobs); this is the one job that couldn't fit, and it is the backpressure boundary: while it is set the
	// control loop stops reading new control messages (so ReportChangeset blocks upstream), but it keeps draining
	// results, which keeps the loop⇄hasher pair deadlock-free.
	hashAwaitingDispatch *hashWork

	// The highest block number written to disk. Once a block has been flushed, reports for blocks at or below
	// this are discarded: they are already on disk, so a late or duplicate report (e.g. a hash that arrives for
	// a block the overflow path already force-flushed) must not resurrect or duplicate it. This only ever
	// advances within a session; to roll back, close the logger and open a new one, which starts fresh with
	// nothing flushed.
	flushedHighWater uint64

	// Guards flushedHighWater's zero value: until the first flush it is meaningless, since block 0 is a valid
	// height. Set true by the first emit and never reset within a session.
	hasFlushedAtLeastOnce bool
}

// NewHashLogger creates a HashLogger that writes to config.Path.
func NewHashLogger(config *HashLoggerConfig) (HashLogger, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid hash logger config: %w", err)
	}

	if err := ensureDirectoryExists(config.Path, true); err != nil {
		return nil, fmt.Errorf("failed to ensure hash log directory %s: %w", config.Path, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	senderCtx, senderCancel := context.WithCancel(ctx)

	// The recorded columns are the caller-reported types, with the logger-owned changeset column prepended when
	// changeset hashing is enabled.
	var hashTypes []string
	if !config.DisableChangesetHashing {
		hashTypes = append(hashTypes, ChangesetHashType)
	}
	hashTypes = append(hashTypes, config.HashTypes...)
	hashTypeSet := make(map[string]struct{}, len(hashTypes))
	for _, hashType := range hashTypes {
		hashTypeSet[hashType] = struct{}{}
	}

	h := &hashLoggerImpl{
		directory:                config.Path,
		version:                  sanitizeVersion(config.Version),
		hashTypes:                hashTypes,
		hashTypeSet:              hashTypeSet,
		changesetHashingDisabled: config.DisableChangesetHashing,
		targetFileSize:           uint64(config.TargetFileSize),
		blocksToRetain:           uint64(config.BlocksToRetain),
		maxDiskSize:              uint64(config.MaxDiskSize),
		maxBufferedBlocks:        uint64(config.MaxBufferedBlocks),
		controlChan:              make(chan controlMessage, config.ControlBufferSize),
		writerChan:               make(chan writerMessage, config.WriteBufferSize),
		ctx:                      ctx,
		cancel:                   cancel,
		senderCtx:                senderCtx,
		senderCancel:             senderCancel,
		sealedFiles:              make(map[uint64]*sealedFileInfo),
		pendingBlocks:            make(map[uint64]*HashLog),
		blocksWithPendingHashes:  make(map[uint64]struct{}),
	}
	if !h.changesetHashingDisabled {
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

	if !h.changesetHashingDisabled {
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
		size := uint64(info.Size()) //nolint:gosec // file size is non-negative
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

// RegisterHashType adds a caller-reported hash column. May be called at any time, including after blocks
// have been logged: the logger flushes complete blocks to the current file, seals it, and opens a fresh
// file whose header includes the new column, so every file's header matches its rows. Registering a type
// that is already present is a no-op (no rotation). See the HashLogger interface for the full contract.
func (h *hashLoggerImpl) RegisterHashType(hashType string) error {
	if !h.changesetHashingDisabled && hashType == ChangesetHashType {
		return fmt.Errorf("hash type %q is reserved for the logger-computed changeset column", hashType)
	}
	if !legalHashTypeRegex.MatchString(hashType) {
		return fmt.Errorf("hash type %q contains illegal characters (must match %s)",
			hashType, legalHashTypeRegex.String())
	}
	if _, ok := h.hashTypeSet[hashType]; ok {
		return nil // already registered; idempotent no-op (no rotation)
	}
	return h.sendColumnChange(hashType, true)
}

// UnregisterHashType removes a caller-reported hash column, rotating to a fresh file whose header omits
// it (same flush/seal/open sequence as RegisterHashType). Removing a type that is not present is a no-op.
// The reserved changeset column cannot be removed.
func (h *hashLoggerImpl) UnregisterHashType(hashType string) error {
	if !h.changesetHashingDisabled && hashType == ChangesetHashType {
		return fmt.Errorf("hash type %q is the logger-computed changeset column and cannot be removed", hashType)
	}
	if _, ok := h.hashTypeSet[hashType]; !ok {
		return nil // not registered; idempotent no-op (no rotation)
	}
	return h.sendColumnChange(hashType, false)
}

// sendColumnChange forwards a column add/remove to the control loop and waits for it to be applied (the
// loop flushes/seals/rotates and updates hashTypes/hashTypeSet before acking). The synchronous handshake
// guarantees that a subsequent ReportHash for the new column is accepted, and establishes happens-before
// for the caller's later reads of hashTypeSet. If the logger is shutting down before the change is
// applied, it returns the relevant context error so the caller knows the registration did not land.
func (h *hashLoggerImpl) sendColumnChange(hashType string, add bool) error {
	if h.closed.Load() {
		return fmt.Errorf("hash logger is closed")
	}
	done := make(chan struct{})
	select {
	case h.controlChan <- controlMessage{kind: ctrlColumnChange, hashType: hashType, add: add, done: done}:
		select {
		case <-done:
			return nil
		case <-h.ctx.Done():
			return h.ctx.Err()
		}
	case <-h.senderCtx.Done():
		return h.senderCtx.Err()
	}
}

func (h *hashLoggerImpl) ReportChangeset(blockNumber uint64, cs []*proto.NamedChangeSet) {
	// Calling Report* after Close() violates the contract; fail fast (no-op) rather than risk a send on a
	// closed channel.
	if h.closed.Load() {
		return
	}
	if h.changesetHashingDisabled {
		return
	}
	// A nil change set means the caller is opting out of changeset hashing for this block: record a nil changeset hash
	// (which still completes the block) without bothering the hasher thread. An empty (non-nil) change set is a
	// legitimate no-change block and falls through to be hashed normally (yielding the hash of the empty changeset).
	if cs == nil {
		h.sendControl(
			controlMessage{kind: ctrlHashReport, blockNumber: blockNumber, hashType: ChangesetHashType, hash: nil})
		return
	}
	// Blocking send to the control loop, which dispatches the change set to the hasher. The changeset is never
	// dropped, so the recorded changeset hash always reflects the change set that was reported.
	h.sendControl(controlMessage{kind: ctrlChangesetRequest, blockNumber: blockNumber, cs: cs})
}

func (h *hashLoggerImpl) ReportHash(blockNumber uint64, hashType string, hash []byte) error {
	// Calling Report* after Close() violates the contract; fail fast rather than risk a send on a closed
	// channel.
	if h.closed.Load() {
		return fmt.Errorf("hash logger is closed")
	}
	// The changeset column is logger-owned: it is computed internally from ReportChangeset, not supplied by the caller.
	// Reject it here so a caller can never clobber (or race) the computed changeset hash via ReportHash.
	if !h.changesetHashingDisabled && hashType == ChangesetHashType {
		return fmt.Errorf("hash type %q is reserved for the logger-computed changeset; use ReportChangeset", hashType)
	}
	if _, ok := h.hashTypeSet[hashType]; !ok {
		return fmt.Errorf("unknown hash type %q", hashType)
	}
	// Blocking send to the control loop, which normally drains controlChan quickly; it can backpressure only
	// if the downstream writer is itself stalled on a slow disk.
	h.sendControl(controlMessage{kind: ctrlHashReport, blockNumber: blockNumber, hashType: hashType, hash: hash})
	return nil
}

func (h *hashLoggerImpl) Close() error {
	h.closeOnce.Do(func() {
		// Reject further Report* calls (best effort; senderCtx is the real backstop).
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
			// Hard stop (error path); abandon in-flight changesets.
			return
		case work, ok := <-h.hashChan:
			if !ok {
				// The control loop closed hashChan after draining all in-flight changesets; nothing left to do.
				return
			}
			result := hashResult{blockNumber: work.blockNumber, hash: hashChangeset(work.cs)}
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
		if h.hashAwaitingDispatch == nil {
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
			case res := <-h.hashResultChan: // nil channel when changeset hashing is disabled; case never fires
				h.applyChangesetResult(res)
			}
		} else {
			// A changeset is waiting to be dispatched: offer it to the hasher while still draining results, so the
			// control loop and hasher can never deadlock sending to each other.
			select {
			case <-h.ctx.Done():
				return
			case res := <-h.hashResultChan:
				h.applyChangesetResult(res)
			case h.hashChan <- *h.hashAwaitingDispatch:
				h.hashAwaitingDispatch = nil
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
	case ctrlChangesetRequest:
		h.handleChangesetRequest(msg.blockNumber, msg.cs)
	case ctrlColumnChange:
		h.handleColumnChange(msg.hashType, msg.add)
		close(msg.done)
	}
}

// handleColumnChange adds or removes a hash column and rotates to a fresh file whose header reflects the
// new column set. Complete blocks are flushed to the current file first so they keep the header that
// matches their columns; the rotation directive is then enqueued behind them, and subsequent blocks land
// in the new file. Any still-incomplete buffered block carries over and is written to the new file once
// complete (callers are expected to change columns at block boundaries, where nothing is buffered).
func (h *hashLoggerImpl) handleColumnChange(hashType string, add bool) {
	_, present := h.hashTypeSet[hashType]
	if add == present {
		return // already in the desired state; nothing to do (defensive — callers pre-check)
	}

	// Flush everything that is complete under the current columns to the current file.
	h.drainComplete()

	// Always build a fresh slice rather than appending in place: the writer may still hold the current
	// hashTypes backing array (it became the initial file's header), so mutating it would race.
	if add {
		updated := make([]string, len(h.hashTypes), len(h.hashTypes)+1)
		copy(updated, h.hashTypes)
		h.hashTypes = append(updated, hashType)
		h.hashTypeSet[hashType] = struct{}{}
	} else {
		delete(h.hashTypeSet, hashType)
		remaining := make([]string, 0, len(h.hashTypes)-1)
		for _, t := range h.hashTypes {
			if t != hashType {
				remaining = append(remaining, t)
			}
		}
		h.hashTypes = remaining
	}

	// Direct the writer to seal the current file and open a fresh one with the new header. Pass a copy so a
	// later column change cannot mutate the slice the writer holds.
	newColumns := append([]string(nil), h.hashTypes...)
	h.blockingSendToWriter(writerMessage{rotate: true, hashTypes: newColumns})
}

// handleHashReport records a caller-reported hash, discarding it if the block has already been flushed.
func (h *hashLoggerImpl) handleHashReport(blockNumber uint64, hashType string, hash []byte) {
	if h.hasFlushedAtLeastOnce && blockNumber <= h.flushedHighWater {
		return // already on disk: a duplicate/late report, or a re-execution without reopening the logger
	}
	h.ensurePending(blockNumber).Hashes[hashType] = hash
}

// handleChangesetRequest records that a block is awaiting a changeset hash and holds the work for dispatch to
// the hasher.
func (h *hashLoggerImpl) handleChangesetRequest(blockNumber uint64, cs []*proto.NamedChangeSet) {
	if h.hasFlushedAtLeastOnce && blockNumber <= h.flushedHighWater {
		return // already on disk; discard
	}
	// Create the pending entry now so minPendingKey accounts for changeset-only blocks and the overflow path can
	// see that the oldest block is awaiting a changeset.
	h.ensurePending(blockNumber)
	h.blocksWithPendingHashes[blockNumber] = struct{}{}
	h.hashAwaitingDispatch = &hashWork{blockNumber: blockNumber, cs: cs}
}

// applyChangesetResult records a computed changeset hash and clears the block's pending-changeset marker.
func (h *hashLoggerImpl) applyChangesetResult(res hashResult) {
	delete(h.blocksWithPendingHashes, res.blockNumber)
	if h.hasFlushedAtLeastOnce && res.blockNumber <= h.flushedHighWater {
		return // the block was already flushed (e.g. force-flushed by the overflow path); discard the stale changeset
	}
	h.ensurePending(res.blockNumber).Hashes[ChangesetHashType] = res.hash
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
// a block awaiting an in-flight changeset (its changeset is on the way), even if that leaves the buffer over the bound.
func (h *hashLoggerImpl) flushProgress() {
	for {
		h.drainComplete()
		if uint64(len(h.pendingBlocks)) <= h.maxBufferedBlocks {
			return
		}
		oldest := minPendingKey(h.pendingBlocks)
		if _, awaitingChangeset := h.blocksWithPendingHashes[oldest]; awaitingChangeset {
			return // don't force-flush a block whose changeset is still being computed
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

// drainInFlightChangesets blocks until every dispatched changeset has returned, applying each result, so no
// changeset result can arrive after the buffer has been flushed. Used on shutdown.
func (h *hashLoggerImpl) drainInFlightChangesets() {
	for len(h.blocksWithPendingHashes) > 0 {
		select {
		case res := <-h.hashResultChan:
			h.applyChangesetResult(res)
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
	h.hasFlushedAtLeastOnce = true
}

// gracefulDrain flushes all remaining completable work on a clean shutdown, then closes the downstream channels
// so the hasher and writer drain and exit.
func (h *hashLoggerImpl) gracefulDrain() {
	h.drainInFlightChangesets()
	h.flushCompleteOnShutdown()
	if !h.changesetHashingDisabled {
		close(h.hashChan)
	}
	close(h.writerChan)
}

// flushCompleteOnShutdown writes every complete block still buffered, in increasing block order, and discards
// any incomplete one. Unlike the overflow path — which must force-flush incomplete blocks to bound memory mid-run
// — a clean shutdown never writes a partial record: a block that is missing a hash type at close would read back
// with that column nil and surface as a spurious divergence during comparison, so it is dropped instead.
func (h *hashLoggerImpl) flushCompleteOnShutdown() {
	blocks := make([]uint64, 0, len(h.pendingBlocks))
	for blockNumber := range h.pendingBlocks {
		blocks = append(blocks, blockNumber)
	}
	sort.Slice(blocks, func(i int, j int) bool { return blocks[i] < blocks[j] })

	discarded := 0
	for _, blockNumber := range blocks {
		if len(h.pendingBlocks[blockNumber].Hashes) < len(h.hashTypes) {
			discarded++
			continue
		}
		h.emit(blockNumber)
	}
	if discarded > 0 {
		logger.Info("discarded incomplete blocks on clean shutdown", "count", discarded)
	}
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
			if msg.rotate {
				// Column change: seal the current file and open a fresh one with the new header.
				if err := h.rotateToColumns(msg.hashTypes); err != nil {
					h.fail(err)
					return
				}
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
	if err := h.mutableFile.write(log); err != nil {
		return err
	}
	if log.BlockNumber > h.latestBlock {
		h.latestBlock = log.BlockNumber
	}
	if h.mutableFile.size >= h.targetFileSize {
		// Size-based rotation keeps the current column set (the new file has the same header).
		if err := h.rotateToColumns(h.mutableFile.hashTypes); err != nil {
			return err
		}
	}
	return nil
}

// rotateToColumns seals the current mutable file, records its bookkeeping, opens a fresh mutable file
// with the given columns as its header, and runs GC. An empty current file (no blocks written) is
// removed by recordSealedFile rather than sealed, and its file index is reused for the new file rather
// than burned — so a burst of column changes between blocks (e.g. the startup registration of every
// category) leaves neither orphan files nor index gaps behind. The column set is owned by the writer
// (mutableFile.hashTypes); the control loop hands new columns in via the rotate message, so the two
// goroutines never share the slice.
func (h *hashLoggerImpl) rotateToColumns(columns []string) error {
	hadBlocks, err := h.recordSealedFile()
	if err != nil {
		return err
	}
	// Only consume the index if the sealed file actually held blocks. An unwritten file was just removed
	// and recorded nothing, so reuse its index. The reused index is always > every sealed index (it is
	// the current mutable index), so this never collides with an existing sealed file.
	if hadBlocks {
		h.mutableLogFileIndex++
	}
	newFile, err := newHashLogFile(h.directory, h.mutableLogFileIndex, h.version, columns)
	if err != nil {
		return fmt.Errorf("failed to open new mutable hash log file: %w", err)
	}
	h.mutableFile = newFile
	h.runGC()
	return nil
}

func (h *hashLoggerImpl) sealMutableAndGC() error {
	if _, err := h.recordSealedFile(); err != nil {
		return err
	}
	h.runGC()
	return nil
}

// recordSealedFile seals the current mutable file and, if it held any blocks, adds it to the sealed-file
// bookkeeping. An empty file is removed by close() and leaves no bookkeeping behind. The returned bool
// reports whether the file held any blocks (and thus consumed its file index).
func (h *hashLoggerImpl) recordSealedFile() (bool, error) {
	hadBlocks := h.mutableFile.hasBlocks
	idx := h.mutableFile.index
	first := h.mutableFile.firstBlockIndex
	last := h.mutableFile.lastBlockIndex
	size := h.mutableFile.size

	if err := h.mutableFile.close(); err != nil {
		return false, fmt.Errorf("failed to seal hash log file: %w", err)
	}
	if !hadBlocks {
		return false, nil
	}
	h.sealedFiles[idx] = &sealedFileInfo{
		name:       sealedFileName(idx, first, last, h.version),
		firstBlock: first,
		lastBlock:  last,
		size:       size,
	}
	h.currentDiskSpaceUsed += size
	return true, nil
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

		// Retain exactly the most-recent blocksToRetain blocks: a file is over the window once its newest block
		// is more than blocksToRetain-1 behind the latest. Written as an addition to avoid unsigned underflow
		// when latestBlock < blocksToRetain (in which case nothing is over the window). A zero limit disables
		// that dimension entirely (no block-count window / no disk cap).
		overBlockRetention := h.blocksToRetain > 0 && info.lastBlock+h.blocksToRetain <= h.latestBlock
		overSizeCap := h.maxDiskSize > 0 && h.currentDiskSpaceUsed > h.maxDiskSize
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
