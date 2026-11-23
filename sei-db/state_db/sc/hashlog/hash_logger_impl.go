package hashlog

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/seilog"
)

var _ HashLogger = (*hashLoggerImpl)(nil)

var logger = seilog.NewLogger("db", "state-db", "sc", "hashlog")

// Column names for the hashes a flatKV store publishes: the store-wide root, one per data database, formed by
// joining the DB prefix with that database's name, and one per module, formed by joining the module prefix with
// "<database>/<module>". flatkv.HashTypes builds the list a node declares from the first two.
const (
	FlatKVRootHashType     = "flatKV/root"
	FlatKVDBHashPrefix     = "flatKV/db/"
	FlatKVModuleHashPrefix = "flatKV/module/"
)

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
// offloads the write to the HashLogWriter.
//
//	                                            ┌──────files──────▶ HashLogWriter
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

// A standard hash logger implementation.
type hashLoggerImpl struct {
	// When true, changeset hashing is disabled: no hasher thread, ReportChangeset is a no-op, and no changeset column is
	// recorded or awaited.
	changesetHashingDisabled bool

	// The most blocks the control loop will buffer before force-flushing the oldest incomplete block to bound
	// memory.
	maxBufferedBlocks uint64

	// For sending work to the control loop (the hub for all caller entry points).
	controlChan chan controlMessage

	// Writes the blocks the control loop completes. Blocks are sent in the order they are emitted, so blocks
	// before a column change land in the old file and blocks after it in the new one.
	files *HashLogWriter

	// For dispatching changeset work from the control loop to the hasher. Nil when changeset hashing is disabled.
	hashChan chan hashWork

	// For the hasher to return computed changeset hashes to the control loop. Nil when changeset hashing is disabled.
	hashResultChan chan hashResult

	// The hard-stop context that the hasher and the control loop watch. Cancelled by fail() on a fatal error and by
	// Close() once everything has drained.
	ctx context.Context

	// Cancels ctx.
	cancel context.CancelFunc

	// A child of ctx that the controlChan producers (ReportHash/ReportChangeset) watch. The control loop cancels it
	// once it stops reading controlChan, so an in-flight or future push aborts rather than deadlocking. Because
	// controlChan is never closed, those pushes can never panic on a closed channel.
	senderCtx context.Context

	// Cancels senderCtx.
	senderCancel context.CancelFunc

	// Tracks the hasher and control-loop goroutines so Close() can wait for them to exit.
	wg sync.WaitGroup

	// Guarantees the Close() shutdown sequence runs at most once.
	closeOnce sync.Once

	// Set by Close() so subsequent Report* calls fail fast (best effort; senderCtx is the real backstop).
	closed atomic.Bool

	// The first unrecoverable background-goroutine error, surfaced to the caller by Close().
	asyncErr atomic.Pointer[error]

	// The following fields are the control loop's private bookkeeping, owned exclusively by the control loop
	// goroutine, so they need no synchronization.

	// The hash columns recorded per block, including the changeset column when changeset hashing is enabled. The
	// loop reads len(hashTypes) to decide whether a block is complete.
	hashTypes []string

	// The membership set over hashTypes, for deciding whether a reported column already exists.
	hashTypeSet map[string]struct{}

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

	// The recorded columns are the caller-reported types, plus the logger-owned changeset column when changeset
	// hashing is enabled.
	var hashTypes []string
	if !config.DisableChangesetHashing {
		hashTypes = append(hashTypes, ChangesetHashType)
	}
	hashTypes = append(hashTypes, config.HashTypes...)
	hashTypeSet := make(map[string]struct{}, len(hashTypes))
	for _, hashType := range hashTypes {
		hashTypeSet[hashType] = struct{}{}
	}

	files, err := NewHashLogWriter(config)
	if err != nil {
		return nil, fmt.Errorf("failed to open hash log writer: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	senderCtx, senderCancel := context.WithCancel(ctx)

	h := &hashLoggerImpl{
		hashTypes:                hashTypes,
		hashTypeSet:              hashTypeSet,
		changesetHashingDisabled: config.DisableChangesetHashing,
		maxBufferedBlocks:        uint64(config.MaxBufferedBlocks),
		controlChan:              make(chan controlMessage, config.ControlBufferSize),
		files:                    files,
		ctx:                      ctx,
		cancel:                   cancel,
		senderCtx:                senderCtx,
		senderCancel:             senderCancel,
		pendingBlocks:            make(map[uint64]*HashLog),
		blocksWithPendingHashes:  make(map[uint64]struct{}),
	}
	if !h.changesetHashingDisabled {
		h.hashChan = make(chan hashWork, config.HashBufferSize)
		h.hashResultChan = make(chan hashResult, config.HashBufferSize)
	}

	if !h.changesetHashingDisabled {
		h.wg.Add(1)
		go h.hasher()
	}
	h.wg.Add(1)
	go h.controlLoop()

	return h, nil
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
	return h.sendColumnChange(hashType, true)
}

// UnregisterHashType removes a caller-reported hash column, rotating to a fresh file whose header omits
// it (same flush/seal/open sequence as RegisterHashType). Removing a type that is not present is a no-op.
// The reserved changeset column cannot be removed.
func (h *hashLoggerImpl) UnregisterHashType(hashType string) error {
	if !h.changesetHashingDisabled && hashType == ChangesetHashType {
		return fmt.Errorf("hash type %q is the logger-computed changeset column and cannot be removed", hashType)
	}
	return h.sendColumnChange(hashType, false)
}

// sendColumnChange forwards a column add/remove to the control loop and waits for it to be applied (the
// loop flushes/seals/rotates and updates hashTypes/hashTypeSet before acking). Waiting is what lets a
// caller declare a column before reporting anything, so the first file's header already carries it and no
// early block is written without it. If the logger is shutting down before the change is applied, it
// returns the relevant context error so the caller knows the registration did not land.
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
	// An unregistered type is not rejected here: whether it is registered is the control loop's to know, and
	// asking would mean reading the loop's state from this goroutine. The loop creates the column instead.
	//
	// Blocking send to the control loop, which normally drains controlChan quickly; it can backpressure only
	// if the downstream writer is itself stalled on a slow disk.
	h.sendControl(controlMessage{kind: ctrlHashReport, blockNumber: blockNumber, hashType: hashType, hash: hash})
	return nil
}

// HashListener records one block's flatKV hashes: the store-wide root and each data database's root.
// Its signature is gigatypes.HashListener, so it registers as one directly:
// stateDB.RegisterHashListener(hashLogger.HashListener).
func (h *hashLoggerImpl) HashListener(_ context.Context, blockNumber int64, hash *lthash.BlockHash) error {
	block := uint64(blockNumber) //nolint:gosec // commit versions are non-negative

	root := hash.Global.Checksum()
	if err := h.ReportHash(block, FlatKVRootHashType, root[:]); err != nil {
		return fmt.Errorf("record the flatkv root hash of block %d: %w", block, err)
	}
	for dataDB, dbHash := range hash.PerDB {
		checksum := dbHash.Checksum()
		if err := h.ReportHash(block, FlatKVDBHashPrefix+dataDB, checksum[:]); err != nil {
			return fmt.Errorf("record the flatkv %s hash of block %d: %w", dataDB, block, err)
		}
	}
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

// handleColumnChange adds or removes a hash column. Blocks complete under the old column set are emitted first, so
// they are written under it; every block emitted afterwards, including one still incomplete now, is written under
// the new set.
func (h *hashLoggerImpl) handleColumnChange(hashType string, add bool) {
	_, present := h.hashTypeSet[hashType]
	if add == present {
		return // already in the desired state; nothing to do (defensive — callers pre-check)
	}

	h.drainComplete()

	if add {
		h.hashTypes = append(h.hashTypes, hashType)
		h.hashTypeSet[hashType] = struct{}{}
	} else {
		delete(h.hashTypeSet, hashType)
		h.hashTypes = slices.DeleteFunc(h.hashTypes, func(t string) bool { return t == hashType })
	}
}

// handleHashReport records a caller-reported hash, discarding it if the block has already been flushed.
func (h *hashLoggerImpl) handleHashReport(blockNumber uint64, hashType string, hash []byte) {
	// Adopting the column first, and re-checking the high water after, is what keeps this block from being
	// written twice: adopting flushes every block that is complete under the old column set, which can
	// include this one, and ensurePending would then rebuild the entry that flush just emitted.
	if !h.adoptReportedColumn(hashType) {
		return
	}
	if h.hasFlushedAtLeastOnce && blockNumber <= h.flushedHighWater {
		return // already on disk: a duplicate/late report, or a re-execution without reopening the logger
	}
	h.ensurePending(blockNumber).Hashes[hashType] = hash
}

// adoptReportedColumn makes hashType a recorded column if it is not one already, reporting whether a hash
// may be recorded under it.
//
// A caller reporting a column that was never registered is a wiring mistake, and this is a logging
// utility: losing the hash would trade an observability problem for a blind spot. Adding the column keeps
// the hash and self-heals a registration that never happened, and it bounds the complaint to one line per
// column rather than one per block.
//
// An illegal name is the exception, and is dropped. Column names are joined into the CSV header and rows
// with no quoting, so a name carrying a separator would shift every column in the archive.
func (h *hashLoggerImpl) adoptReportedColumn(hashType string) bool {
	if _, ok := h.hashTypeSet[hashType]; ok {
		return true
	}
	if !legalHashTypeRegex.MatchString(hashType) {
		logger.Error("discarding a hash reported under an illegal column name",
			"hashType", hashType, "mustMatch", legalHashTypeRegex.String())
		return false
	}
	logger.Warn("recording a hash reported under a column that was never registered; adding it",
		"hashType", hashType)
	h.handleColumnChange(hashType, true)
	return true
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

// emit hands a single block to the writer, with a nil hash for each column the block is missing, and records it as
// flushed.
func (h *hashLoggerImpl) emit(blockNumber uint64) {
	log := h.pendingBlocks[blockNumber]
	delete(h.pendingBlocks, blockNumber)
	delete(h.blocksWithPendingHashes, blockNumber)
	hashes := make(map[string][]byte, len(h.hashTypes))
	for _, hashType := range h.hashTypes {
		hashes[hashType] = log.Hashes[hashType]
	}
	if err := h.files.Write(blockNumber, hashes); err != nil {
		h.fail(err)
	}
	h.flushedHighWater = blockNumber
	h.hasFlushedAtLeastOnce = true
}

// gracefulDrain flushes all remaining completable work on a clean shutdown, then closes the hasher's channel and
// the writer so both drain and exit.
func (h *hashLoggerImpl) gracefulDrain() {
	h.drainInFlightChangesets()
	h.flushCompleteOnShutdown()
	if !h.changesetHashingDisabled {
		close(h.hashChan)
	}
	if err := h.files.Close(); err != nil {
		h.fail(err)
	}
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
