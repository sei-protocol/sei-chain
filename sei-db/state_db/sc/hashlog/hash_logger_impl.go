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

// A diff to be hashed on the background hasher thread.
type diffMessage struct {
	blockNumber uint64
	cs          []*proto.NamedChangeSet
}

// A single hash report destined for the control loop.
type controlMessage struct {
	blockNumber uint64
	hashType    string
	hash        []byte
}

// Bookkeeping for a sealed hash log file (owned by the writer goroutine).
type sealedFileInfo struct {
	name       string
	firstBlock uint64
	lastBlock  uint64
	size       uint64
}

// A standard hash logger implementation.
//
// Work flows through three goroutines so that reporting never blocks the caller's commit path:
//
//	ReportDiff ──▶ hashChan ──▶ hasher ───┐
//	                                      ├──▶ controlChan ──▶ controlLoop ──▶ writerChan ──▶ writer
//	ReportHash ───────────────────────────┘
//
// The hasher computes diff hashes off the hot path. The control loop assembles a complete HashLog per block,
// emits blocks in order, and detects rollbacks. The writer owns all on-disk state.
//
// Load is shed only where a stage can fall behind a slow downstream resource: the hasher channel (a flood of
// diffs to hash) and the writer channel (a slow disk). ReportDiff drops a diff (recording a nil hash) when the
// hasher channel is full; the control loop drops a block when the writer channel is full. ReportHash, by
// contrast, does a blocking send: the control loop drains its channel quickly (its own shedding happens
// downstream), so this cannot stall the caller's commit path. Report* must not be called after Close().
type hashLoggerImpl struct {
	// Immutable configuration captured at construction.
	directory    string
	version      string // sanitized
	hashTypes    []string
	hashTypeSet  map[string]struct{}
	diffHashType string // DiffHashingDisabled if diff hashing is disabled

	maxBlockDelay  uint
	targetFileSize uint64
	blocksToRetain uint64
	maxDiskSize    uint64

	// For sending work to the control loop.
	controlChan chan controlMessage

	// For sending work to the writer thread.
	writerChan chan *HashLog

	// For sending work to the background hasher thread. Nil if diff hashing is disabled.
	hashChan chan diffMessage

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
	asyncErr  atomic.Pointer[error]

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

	hashTypes := append([]string(nil), config.HashTypes...)
	hashTypeSet := make(map[string]struct{}, len(hashTypes))
	for _, hashType := range hashTypes {
		hashTypeSet[hashType] = struct{}{}
	}

	h := &hashLoggerImpl{
		directory:      config.Path,
		version:        sanitizeVersion(config.Version),
		hashTypes:      hashTypes,
		hashTypeSet:    hashTypeSet,
		diffHashType:   config.DiffHashType,
		maxBlockDelay:  config.MaxBlockDelay,
		targetFileSize: uint64(config.TargetFileSize),
		blocksToRetain: uint64(config.BlocksToRetain),
		maxDiskSize:    uint64(config.MaxDiskSize),
		controlChan:    make(chan controlMessage, config.ControlBufferSize),
		writerChan:     make(chan *HashLog, config.WriteBufferSize),
		ctx:            ctx,
		cancel:         cancel,
		sealedFiles:    make(map[uint64]*sealedFileInfo),
	}
	if h.diffHashType != DiffHashingDisabled {
		h.hashChan = make(chan diffMessage, config.HashBufferSize)
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

	if h.diffHashType != DiffHashingDisabled {
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
	if h.diffHashType == DiffHashingDisabled {
		return
	}
	// A nil change set means the caller is opting out of diff hashing for this block: record a nil diff hash
	// (which still completes the block) without bothering the hasher thread. An empty (non-nil) change set is a
	// legitimate no-change block and falls through to be hashed normally (yielding the hash of the empty diff).
	if cs == nil {
		h.sendControl(controlMessage{blockNumber: blockNumber, hashType: h.diffHashType, hash: nil})
		return
	}
	// Send the diff to the hasher thread. If the hasher's channel is full, drop the diff and notify the
	// control loop with a nil hash so the block can still complete (load shedding).
	select {
	case h.hashChan <- diffMessage{blockNumber: blockNumber, cs: cs}:
	default:
		h.sendControl(controlMessage{blockNumber: blockNumber, hashType: h.diffHashType, hash: nil})
	}
}

func (h *hashLoggerImpl) ReportHash(blockNumber uint64, hashType string, hash []byte) error {
	if _, ok := h.hashTypeSet[hashType]; !ok {
		return fmt.Errorf("unknown hash type %q", hashType)
	}
	// No load shedding here: the control loop drains controlChan quickly (it sheds further downstream, at the
	// hasher and writer channels), so this blocking send cannot stall the caller's commit path for long.
	h.sendControl(controlMessage{blockNumber: blockNumber, hashType: hashType, hash: hash})
	return nil
}

func (h *hashLoggerImpl) Close() error {
	h.closeOnce.Do(func() {
		// Contract: the caller has stopped reporting. Closing the head of the pipeline triggers a staged drain
		// in which each stage flushes its work and closes the next stage's channel.
		if h.diffHashType != DiffHashingDisabled {
			close(h.hashChan)
		} else {
			close(h.controlChan)
		}
		h.wg.Wait()
	})
	if err := h.asyncErr.Load(); err != nil {
		return *err
	}
	return nil
}

// sendControl forwards a message to the control loop, giving up if the logger is shutting down (the channel is
// never closed while a non-control-loop sender may run, so this cannot send on a closed channel).
func (h *hashLoggerImpl) sendControl(msg controlMessage) {
	select {
	case h.controlChan <- msg:
	case <-h.ctx.Done():
	}
}

func (h *hashLoggerImpl) hasher() {
	defer h.wg.Done()
	for {
		select {
		case <-h.ctx.Done():
			// Hard stop (error path); abandon in-flight diffs. controlChan is left open and is never closed
			// on this path, so other senders cannot panic.
			return
		case msg, ok := <-h.hashChan:
			if !ok {
				// Graceful drain complete. The hasher is the last controlChan producer, so it closes it.
				close(h.controlChan)
				return
			}
			h.sendControl(controlMessage{
				blockNumber: msg.blockNumber,
				hashType:    h.diffHashType,
				hash:        hashDiff(msg.cs),
			})
		}
	}
}

func (h *hashLoggerImpl) controlLoop() {
	defer h.wg.Done()

	pending := make(map[uint64]*HashLog)
	var lastEmitted uint64
	lastEmittedValid := false

	for {
		select {
		case <-h.ctx.Done():
			return
		case msg, ok := <-h.controlChan:
			if !ok {
				// Graceful drain: deliver everything we have, in order, reliably (the caller has stopped, so
				// blocking the writer here cannot stall a commit path), then close the writer.
				for len(pending) > 0 {
					oldest := minPendingKey(pending)
					h.blockingSendToWriter(pending[oldest])
					delete(pending, oldest)
				}
				close(h.writerChan)
				return
			}
			h.handleControlMessage(msg, pending, &lastEmitted, &lastEmittedValid)
		}
	}
}

func (h *hashLoggerImpl) handleControlMessage(
	msg controlMessage,
	pending map[uint64]*HashLog,
	lastEmitted *uint64,
	lastEmittedValid *bool,
) {
	// A report for a block we've already passed indicates a rollback: flush the remaining old-timeline blocks
	// in order, then reset ordering so the re-executed blocks buffer fresh. We don't signal the writer; it
	// detects the regression on its own from the block numbers it receives and rotates to a new file.
	if *lastEmittedValid && msg.blockNumber <= *lastEmitted {
		h.flushAll(pending, lastEmitted, lastEmittedValid)
		*lastEmittedValid = false
	}

	log, ok := pending[msg.blockNumber]
	if !ok {
		log = &HashLog{BlockNumber: msg.blockNumber, Hashes: make(map[string][]byte, len(h.hashTypes))}
		pending[msg.blockNumber] = log
	}
	log.Hashes[msg.hashType] = msg.hash

	h.drainComplete(pending, lastEmitted, lastEmittedValid)

	// Don't buffer forever: once more than MaxBlockDelay blocks are waiting, force-emit the oldest even if
	// it's incomplete, then resume emitting any newly-contiguous complete blocks.
	for uint(len(pending)) > h.maxBlockDelay {
		oldest := minPendingKey(pending)
		h.emit(pending, oldest, lastEmitted, lastEmittedValid)
		h.drainComplete(pending, lastEmitted, lastEmittedValid)
	}
}

// drainComplete emits the contiguous prefix of complete blocks (oldest first), stopping at the first
// incomplete block so that blocks are always written in increasing order.
func (h *hashLoggerImpl) drainComplete(
	pending map[uint64]*HashLog,
	lastEmitted *uint64,
	lastEmittedValid *bool,
) {
	for len(pending) > 0 {
		oldest := minPendingKey(pending)
		if len(pending[oldest].Hashes) < len(h.hashTypes) {
			break
		}
		h.emit(pending, oldest, lastEmitted, lastEmittedValid)
	}
}

// flushAll force-emits every pending block in increasing order, regardless of completeness.
func (h *hashLoggerImpl) flushAll(
	pending map[uint64]*HashLog,
	lastEmitted *uint64,
	lastEmittedValid *bool,
) {
	for len(pending) > 0 {
		oldest := minPendingKey(pending)
		h.emit(pending, oldest, lastEmitted, lastEmittedValid)
	}
}

func (h *hashLoggerImpl) emit(
	pending map[uint64]*HashLog,
	blockNumber uint64,
	lastEmitted *uint64,
	lastEmittedValid *bool,
) {
	log := pending[blockNumber]
	delete(pending, blockNumber)
	h.trySendToWriter(log)
	*lastEmitted = blockNumber
	*lastEmittedValid = true
}

// trySendToWriter delivers a log to the writer, shedding it (dropping) if the writer channel is full. This is
// what keeps the control loop from blocking on a slow disk, which in turn keeps controlChan drained.
func (h *hashLoggerImpl) trySendToWriter(log *HashLog) {
	select {
	case h.writerChan <- log:
	default:
		logger.Debug("writer channel full; dropping block", "block", log.BlockNumber)
	}
}

// blockingSendToWriter delivers a log to the writer, giving up only if the logger is shutting down. Used during
// the graceful drain, where the caller has stopped and the writer is still draining, so it cannot deadlock.
func (h *hashLoggerImpl) blockingSendToWriter(log *HashLog) {
	select {
	case h.writerChan <- log:
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
		case log, ok := <-h.writerChan:
			if !ok {
				// Graceful shutdown: seal the final file so a clean shutdown leaves no ".hlog.u" behind.
				if err := h.sealMutableAndGC(); err != nil {
					h.fail(err)
				}
				return
			}
			if err := h.handleWrite(log); err != nil {
				h.fail(err)
				return
			}
		}
	}
}

func (h *hashLoggerImpl) handleWrite(log *HashLog) error {
	// A block number that doesn't advance indicates a rollback (re-execution of an earlier height). Seal the
	// current file and start a new one so every file's block numbers stay monotonic. The writer detecting this
	// itself (rather than relying on a control signal) means rollback handling survives load shedding.
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
