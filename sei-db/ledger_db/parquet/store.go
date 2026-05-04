package parquet

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/parquet-go/parquet-go"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	dbwal "github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "ledger-db", "parquet")

const (
	maxInt64  = int64(^uint64(0) >> 1)
	maxUint32 = ^uint32(0)

	defaultBlockFlushInterval uint64 = 1
	defaultMaxBlocksPerFile   uint64 = 500
)

var removeFile = os.Remove

// StoreConfig configures the parquet store.
type StoreConfig struct {
	DBDirectory          string
	KeepRecent           int64
	PruneIntervalSeconds int64
	BlockFlushInterval   uint64
	MaxBlocksPerFile     uint64
	TxIndexBackend       string

	// WALConverter, when non-nil, drives synchronous WAL replay during
	// store construction. The function decodes one raw WAL receipt blob
	// into the structured fields the store needs to re-stage it. Only
	// consumed by the v2 store; v1 ignores it. When nil, replay is
	// skipped — used by lower-level tests that drive replay manually.
	WALConverter WALReceiptConverter
}

// WALReceiptConverter decodes a raw WAL receipt blob into the structured
// fields the v2 store needs to re-stage it. logStartIndex carries the
// running per-block log offset so logs from earlier txs in the same block
// don't collide.
type WALReceiptConverter func(blockNumber uint64, receiptBytes []byte, logStartIndex uint) (ReplayReceipt, error)

// ReplayReceipt is one converted WAL entry: the receipt input to re-stage,
// its tx hash, the warmup record returned to the wrapper, and the log
// count consumed (used to advance logStartIndex).
type ReplayReceipt struct {
	Input    ReceiptInput
	TxHash   common.Hash
	Warmup   ReceiptRecord
	LogCount uint
}

// DefaultStoreConfig returns the default store configuration.
func DefaultStoreConfig() StoreConfig {
	return StoreConfig{
		BlockFlushInterval: defaultBlockFlushInterval,
		MaxBlocksPerFile:   defaultMaxBlocksPerFile,
		TxIndexBackend:     dbconfig.ReceiptTxIndexBackendPebble,
	}
}

// ReceiptInput is the input for storing a receipt.
type ReceiptInput struct {
	BlockNumber  uint64
	Receipt      ReceiptRecord
	Logs         []LogRecord
	ReceiptBytes []byte // For WAL
}

// FaultHooks provides optional hook points for fault injection in tests.
// All fields are nil in production. When non-nil, the hook is called at
// the corresponding point in the write path; returning a non-nil error
// aborts the operation and propagates the error to the caller.
type FaultHooks struct {
	AfterWALWrite     func(blockNumber uint64) error // after WAL writes, before parquet apply
	BeforeFlush       func(blockNumber uint64) error // before writing buffers to parquet
	AfterFlush        func(blockNumber uint64) error // after parquet flush, before buffer clear
	AfterCloseWriters func(blockNumber uint64) error // during rotation, after closing old writers
	AfterWALClear     func(blockNumber uint64) error // during rotation, after WAL truncation
}

// Store is the parquet-based receipt store.
type Store struct {
	basePath      string
	receiptWriter *parquet.GenericWriter[ReceiptRecord]
	logWriter     *parquet.GenericWriter[LogRecord]
	receiptFile   *os.File
	logFile       *os.File

	mu               sync.Mutex
	fileStartBlock   uint64
	receiptsBuffer   []ReceiptRecord
	logsBuffer       []LogRecord
	config           StoreConfig
	lastSeenBlock    uint64
	blocksSinceFlush uint64

	Reader          *Reader
	wal             dbwal.GenericWAL[WALEntry]
	latestVersion   atomic.Int64
	earliestVersion atomic.Int64
	closeOnce       sync.Once

	pruneStop chan struct{}

	// WarmupRecords holds receipts recovered from WAL for cache warming.
	WarmupRecords []ReceiptRecord

	// FaultHooks is nil in production. Tests can set this to inject faults
	// at specific points in the write path for crash recovery testing.
	FaultHooks *FaultHooks
}

// NewStore creates a new parquet store.
func NewStore(cfg StoreConfig) (*Store, error) {
	storeCfg := resolveStoreConfig(cfg)

	if err := os.MkdirAll(cfg.DBDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create parquet base directory: %w", err)
	}

	reader, err := NewReaderWithMaxBlocksPerFile(cfg.DBDirectory, storeCfg.MaxBlocksPerFile)
	if err != nil {
		return nil, err
	}

	walDir := filepath.Join(cfg.DBDirectory, "parquet-wal")
	receiptWAL, err := NewWAL(walDir)
	if err != nil {
		return nil, err
	}

	store := &Store{
		basePath:       cfg.DBDirectory,
		receiptsBuffer: make([]ReceiptRecord, 0, 1000),
		logsBuffer:     make([]LogRecord, 0, 10000),
		config:         storeCfg,
		Reader:         reader,
		wal:            receiptWAL,
		pruneStop:      make(chan struct{}),
	}

	if maxBlock, ok, err := reader.MaxReceiptBlockNumber(context.Background()); err != nil {
		return nil, err
	} else if ok {
		latest, err := int64FromUint64(maxBlock)
		if err != nil {
			return nil, err
		}
		store.latestVersion.Store(latest)
		if maxBlock < ^uint64(0) {
			store.fileStartBlock = maxBlock + 1
		}
	}

	store.startPruning(cfg.PruneIntervalSeconds)

	return store, nil
}

func resolveStoreConfig(cfg StoreConfig) StoreConfig {
	resolved := DefaultStoreConfig()
	resolved.DBDirectory = cfg.DBDirectory
	resolved.KeepRecent = cfg.KeepRecent
	resolved.PruneIntervalSeconds = cfg.PruneIntervalSeconds
	if cfg.TxIndexBackend != "" {
		resolved.TxIndexBackend = cfg.TxIndexBackend
	}
	if cfg.BlockFlushInterval > 0 {
		resolved.BlockFlushInterval = cfg.BlockFlushInterval
	}
	if cfg.MaxBlocksPerFile > 0 {
		resolved.MaxBlocksPerFile = cfg.MaxBlocksPerFile
	}
	return resolved
}

// LatestVersion returns the latest version stored.
func (s *Store) LatestVersion() int64 {
	return s.latestVersion.Load()
}

// SetLatestVersion sets the latest version.
func (s *Store) SetLatestVersion(version int64) {
	s.latestVersion.Store(version)
}

// SetEarliestVersion sets the earliest version.
func (s *Store) SetEarliestVersion(version int64) {
	s.earliestVersion.Store(version)
}

// CacheRotateInterval returns the interval at which the cache should rotate.
func (s *Store) CacheRotateInterval() uint64 {
	return s.config.MaxBlocksPerFile
}

// SetBlockFlushInterval overrides the number of blocks buffered before
// flushing to a parquet file. Intended for testing.
func (s *Store) SetBlockFlushInterval(interval uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.BlockFlushInterval = interval
}

// SetMaxBlocksPerFile overrides the rotation interval after construction.
// Intended for tests that need a small boundary so they can exercise rotation
// behavior without writing hundreds of blocks. Not safe to call while writes
// are in flight (rotation / WAL invariants may disagree with the reader until
// the store is quiesced). Concurrent reads remain race-safe under the race
// detector because the reader field is updated under Reader.mu.
func (s *Store) SetMaxBlocksPerFile(n uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.MaxBlocksPerFile = n
	if s.Reader != nil {
		s.Reader.setMaxBlocksPerFile(n)
	}
}

// GetReceiptByTxHash retrieves a receipt by transaction hash via a full scan of
// the closed parquet files tracked by the reader.
func (s *Store) GetReceiptByTxHash(ctx context.Context, txHash common.Hash) (*ReceiptResult, error) {
	return s.Reader.GetReceiptByTxHash(ctx, txHash)
}

// GetReceiptByTxHashInBlock narrows the parquet search to the file containing
// blockNumber, falling back to a full scan on miss.
func (s *Store) GetReceiptByTxHashInBlock(ctx context.Context, txHash common.Hash, blockNumber uint64) (*ReceiptResult, error) {
	return s.Reader.GetReceiptByTxHashInBlock(ctx, txHash, blockNumber)
}

// GetLogs retrieves logs matching the filter.
func (s *Store) GetLogs(ctx context.Context, filter LogFilter) ([]LogResult, error) {
	return s.Reader.GetLogs(ctx, filter)
}

// WriteReceipts writes multiple receipts, batching WAL writes per block.
//
// Rotation fires on aligned block boundaries (blockNumber % MaxBlocksPerFile == 0).
// WAL.Write must run before rotateFileLocked: rotation clears the WAL, so a
// crash between clear and a later WAL write would lose the boundary block.
// ClearWAL preserves the last entry so the just-written boundary entry
// survives the clear and remains replayable.
func (s *Store) WriteReceipts(inputs []ReceiptInput) error {
	if len(inputs) == 0 {
		return nil
	}

	// Group receipts by block number, preserving encounter order.
	type blockBatch struct {
		blockNumber uint64
		receipts    [][]byte
		inputs      []ReceiptInput
	}
	var batches []blockBatch
	batchIdx := make(map[uint64]int)

	for i := range inputs {
		bn := inputs[i].BlockNumber
		if idx, ok := batchIdx[bn]; ok {
			batches[idx].receipts = append(batches[idx].receipts, inputs[i].ReceiptBytes)
			batches[idx].inputs = append(batches[idx].inputs, inputs[i])
		} else {
			batchIdx[bn] = len(batches)
			batches = append(batches, blockBatch{
				blockNumber: bn,
				receipts:    [][]byte{inputs[i].ReceiptBytes},
				inputs:      []ReceiptInput{inputs[i]},
			})
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, b := range batches {
		entry := WALEntry{
			BlockNumber: b.blockNumber,
			Receipts:    b.receipts,
		}
		if err := s.wal.Write(entry); err != nil {
			return err
		}

		if h := s.FaultHooks; h != nil && h.AfterWALWrite != nil {
			if err := h.AfterWALWrite(b.blockNumber); err != nil {
				return err
			}
		}

		if s.receiptWriter != nil && b.blockNumber != s.lastSeenBlock && s.IsRotationBoundary(b.blockNumber) {
			if err := s.rotateFileLocked(b.blockNumber); err != nil {
				return err
			}
		}

		for i := range b.inputs {
			if err := s.applyReceiptLocked(b.inputs[i]); err != nil {
				return err
			}
		}
	}

	return nil
}

// IsRotationBoundary returns true when blockNumber is aligned to the file
// rotation interval (MaxBlocksPerFile). These are the block numbers that start
// a new parquet file.
func (s *Store) IsRotationBoundary(blockNumber uint64) bool {
	if s.config.MaxBlocksPerFile == 0 {
		return false
	}
	return blockNumber%s.config.MaxBlocksPerFile == 0
}

// UpdateLatestVersion updates the latest version if the new value is higher.
func (s *Store) UpdateLatestVersion(version int64) {
	if version > s.latestVersion.Load() {
		s.latestVersion.Store(version)
	}
}

// SimulateCrash abandons the store without flushing or finalizing, mimicking
// an abrupt process termination (e.g. kill -9 or power loss). Specifically
// it skips:
//   - flushLocked(): in-memory buffered receipts are lost
//   - receiptWriter.Close() / logWriter.Close(): parquet footers are never
//     written, leaving on-disk files corrupt/unreadable
//   - receiptFile.Sync() / logFile.Sync(): OS-buffered writes may be lost
//
// The raw os.File.Close() and wal.Close() calls below exist only to release
// file descriptors and locks so the test process can reopen the same directory.
func (s *Store) SimulateCrash() {
	if s.pruneStop != nil {
		close(s.pruneStop)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.receiptFile != nil {
		_ = s.receiptFile.Close()
		s.receiptFile = nil
	}
	if s.logFile != nil {
		_ = s.logFile.Close()
		s.logFile = nil
	}
	s.receiptWriter = nil
	s.logWriter = nil

	_ = s.wal.Close()
	_ = s.Reader.Close()
}

// Close closes the store.
func (s *Store) Close() error {
	var err error
	s.closeOnce.Do(func() {
		if s.pruneStop != nil {
			close(s.pruneStop)
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		if flushErr := s.flushLocked(); flushErr != nil {
			err = flushErr
			return
		}
		if closeErr := s.closeWritersLocked(); closeErr != nil {
			err = closeErr
			return
		}
		if closeErr := s.wal.Close(); closeErr != nil {
			err = closeErr
			return
		}
		if closeErr := s.Reader.Close(); closeErr != nil {
			err = closeErr
			return
		}
	})

	return err
}

// WAL returns the WAL for replay purposes.
func (s *Store) WAL() dbwal.GenericWAL[WALEntry] {
	return s.wal
}

// ApplyReceiptFromReplay applies a receipt during WAL replay. If the block
// number is on a rotation boundary, this rotates the file (without touching the
// WAL) so replay-recovered blocks land in the same aligned files the write path
// would have produced. Skipping WAL truncation here is mandatory: the caller is
// iterating WAL offsets, and truncating mid-iteration would break the scan.
func (s *Store) ApplyReceiptFromReplay(input ReceiptInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.receiptWriter != nil && input.BlockNumber != s.lastSeenBlock && s.IsRotationBoundary(input.BlockNumber) {
		if err := s.rotateFileLockedNoWAL(input.BlockNumber); err != nil {
			return err
		}
	}
	return s.applyReceiptLocked(input)
}

// ObserveEmptyBlock signals that a block with no receipts was committed at
// height. WriteReceipts is the normal place rotation fires, but it is skipped
// for empty blocks — so without this hook a boundary-aligned empty block would
// leave the open file accepting writes past MaxBlocksPerFile and break the
// reader's file-pruning logic (which assumes each file spans at most that many
// blocks). Callers that bump LatestVersion for empty blocks should invoke this
// so the rotation invariant stays in lockstep with the chain.
func (s *Store) ObserveEmptyBlock(height uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Only advance lastSeenBlock for strictly greater heights. Out-of-order
	// observations must not move the cursor backward, or WriteReceipts could
	// mis-handle rotation for blocks already seen.
	if height <= s.lastSeenBlock {
		return nil
	}
	if s.receiptWriter == nil || !s.IsRotationBoundary(height) {
		// No file to rotate yet, or the empty block is not on a boundary.
		// Still track it so a later observation of the same height is a no-op.
		s.lastSeenBlock = height
		return nil
	}
	if err := s.rotateFileLocked(height); err != nil {
		return err
	}
	s.lastSeenBlock = height
	return nil
}

// FileStartBlock returns the current file start block.
func (s *Store) FileStartBlock() uint64 {
	return s.fileStartBlock
}

// ClearWAL truncates the WAL after rotation, preserving the last entry.
// WriteReceipts writes the boundary block's WAL entry before calling rotate,
// and that entry's data has not yet been applied to the new file — losing it
// would lose the block. ObserveEmptyBlock's path has no pending entry, so
// the preserved entry is redundant (already in the closed file) but harmless:
// its blockNumber is < the new fileStartBlock, so replay drops it.
func (s *Store) ClearWAL() error {
	firstOffset, errFirst := s.wal.FirstOffset()
	if errFirst != nil || firstOffset <= 0 {
		return nil
	}
	lastOffset, errLast := s.wal.LastOffset()
	if errLast != nil || lastOffset <= 0 {
		return nil
	}
	if lastOffset <= firstOffset {
		return nil
	}
	if err := s.wal.TruncateBefore(lastOffset); err != nil {
		if strings.Contains(err.Error(), "out of range") {
			return nil
		}
		return fmt.Errorf("failed to truncate parquet WAL before offset %d: %w", lastOffset, err)
	}
	return nil
}

func (s *Store) startPruning(pruneIntervalSeconds int64) {
	if s.config.KeepRecent <= 0 || pruneIntervalSeconds <= 0 {
		return
	}
	go func() {
		for {
			latestVersion := s.latestVersion.Load()
			pruneBeforeBlock := latestVersion - s.config.KeepRecent
			if pruneBeforeBlock > 0 {
				pruned := s.PruneOldFiles(uint64(pruneBeforeBlock))
				if pruned > 0 {
					logger.Info("Pruned parquet file pairs older than block", "pruned-count", pruned, "block", pruneBeforeBlock)
				}
			}

			// Add random jitter (up to 50% of base interval) to avoid thundering herd
			jitter := time.Duration(rand.Float64()*float64(pruneIntervalSeconds)*0.5) * time.Second
			sleepDuration := time.Duration(pruneIntervalSeconds)*time.Second + jitter

			select {
			case <-s.pruneStop:
				return
			case <-time.After(sleepDuration):
				// Continue to next iteration
			}
		}
	}()
}

// PruneOldFiles removes parquet file pairs whose data is entirely before
// pruneBeforeBlock. Returns the number of file pairs removed.
func (s *Store) PruneOldFiles(pruneBeforeBlock uint64) int {
	// Get list of files to prune from the reader
	filesToPrune := s.Reader.GetFilesBeforeBlock(pruneBeforeBlock)
	if len(filesToPrune) == 0 {
		return 0
	}

	prunedCount := 0
	for _, filePair := range filesToPrune {
		// Step 1: Remove from tracking (brief mu.Lock) so new reader
		// snapshots won't include these files.
		if filePair.ReceiptFile != "" {
			s.Reader.RemoveTrackedReceiptFile(filePair.StartBlock)
		}
		if filePair.LogFile != "" {
			s.Reader.RemoveTrackedLogFile(filePair.StartBlock)
		}

		// Step 2: Wait for in-flight readers to finish, then delete.
		// pruneMu.Lock blocks until all current pruneMu.RLock holders
		// (active queries) release.
		s.Reader.pruneMu.Lock()

		receiptRemoved := filePair.ReceiptFile == ""
		if filePair.ReceiptFile != "" {
			if err := removeFile(filePair.ReceiptFile); err != nil && !os.IsNotExist(err) {
				logger.Error("failed to prune receipt file", "file", filePair.ReceiptFile, "err", err)
			} else {
				receiptRemoved = true
			}
		}

		logRemoved := filePair.LogFile == ""
		if filePair.LogFile != "" {
			if err := removeFile(filePair.LogFile); err != nil && !os.IsNotExist(err) {
				logger.Error("failed to prune log file", "file", filePair.LogFile, "err", err)
			} else {
				logRemoved = true
			}
		}

		s.Reader.pruneMu.Unlock()

		// Re-add to tracking if deletion failed (outside pruneMu to
		// avoid holding both locks).
		if !receiptRemoved && filePair.ReceiptFile != "" {
			s.Reader.AddTrackedReceiptFile(filePair.StartBlock)
		}
		if !logRemoved && filePair.LogFile != "" {
			s.Reader.AddTrackedLogFile(filePair.StartBlock)
		}

		if receiptRemoved && logRemoved {
			prunedCount++
		}
	}

	return prunedCount
}

// alignedFileStartBlock returns the parquet filename start block for lazy init:
// the greatest multiple of maxBlocksPerFile not above blockNumber, or blockNumber
// when maxBlocksPerFile is zero (rotation disabled).
func alignedFileStartBlock(blockNumber, maxBlocksPerFile uint64) uint64 {
	if maxBlocksPerFile == 0 {
		return blockNumber
	}
	return (blockNumber / maxBlocksPerFile) * maxBlocksPerFile
}

func (s *Store) applyReceiptLocked(input ReceiptInput) error {
	// Lazy writer initialization: defer file creation until the first receipt
	// arrives. The filename start block is normally snapped down to the
	// rotation interval so it matches Reader assumptions
	// ([start, start+MaxBlocksPerFile)) for pruning and file-range logic.
	// On reopen NewStore pre-sets fileStartBlock to maxBlock+1; if the aligned
	// start falls inside that same rotation window we must keep the preset
	// value, otherwise initWriters would os.Create (and truncate) the existing
	// closed parquet file that still holds the last committed blocks.
	if s.receiptWriter == nil {
		aligned := alignedFileStartBlock(input.BlockNumber, s.config.MaxBlocksPerFile)
		if aligned >= s.fileStartBlock {
			s.fileStartBlock = aligned
		}
		if err := s.initWriters(); err != nil {
			return err
		}
	}

	blockNumber := input.BlockNumber
	if blockNumber != s.lastSeenBlock {
		if s.lastSeenBlock != 0 {
			s.blocksSinceFlush++
		}
		s.lastSeenBlock = blockNumber
	}

	s.receiptsBuffer = append(s.receiptsBuffer, input.Receipt)
	if len(input.Logs) > 0 {
		s.logsBuffer = append(s.logsBuffer, input.Logs...)
	}

	if s.config.BlockFlushInterval > 0 && s.blocksSinceFlush >= s.config.BlockFlushInterval {
		if err := s.flushLocked(); err != nil {
			return err
		}
		s.blocksSinceFlush = 0
	}

	return nil
}

// rotateFileLocked closes the current parquet file, clears older WAL entries
// (ClearWAL keeps the last entry — see WriteReceipts / ClearWAL docs), and opens
// a new file at newBlockNumber. Callers must write newBlockNumber's WAL entry
// before invoking this so rotation never drops the boundary block from the WAL.
func (s *Store) rotateFileLocked(newBlockNumber uint64) error {
	if err := s.rotateFileLockedNoWAL(newBlockNumber); err != nil {
		return err
	}
	if err := s.ClearWAL(); err != nil {
		return err
	}
	if h := s.FaultHooks; h != nil && h.AfterWALClear != nil {
		if err := h.AfterWALClear(newBlockNumber); err != nil {
			return err
		}
	}
	return nil
}

// rotateFileLockedNoWAL performs the file-level rotation without touching the
// WAL. Used during WAL replay where the outer scan would break if entries were
// truncated mid-iteration.
func (s *Store) rotateFileLockedNoWAL(newBlockNumber uint64) error {
	if err := s.flushLocked(); err != nil {
		return err
	}

	oldStartBlock := s.fileStartBlock
	if err := s.closeWritersLocked(); err != nil {
		return err
	}

	if h := s.FaultHooks; h != nil && h.AfterCloseWriters != nil {
		if err := h.AfterCloseWriters(newBlockNumber); err != nil {
			return err
		}
	}

	s.Reader.OnFileRotation(oldStartBlock)
	s.fileStartBlock = newBlockNumber

	// initWriters must come AFTER fileStartBlock is updated so the new file
	// name reflects the new aligned boundary.
	if err := s.initWriters(); err != nil {
		return err
	}

	// Pending buffer data was flushed into the closed file; nothing carries
	// over to the new writer, so reset the flush counter too.
	s.blocksSinceFlush = 0
	return nil
}

func (s *Store) initWriters() error {
	receiptPath := filepath.Join(s.basePath, fmt.Sprintf("receipts_%d.parquet", s.fileStartBlock))
	logPath := filepath.Join(s.basePath, fmt.Sprintf("logs_%d.parquet", s.fileStartBlock))

	// #nosec G304 -- paths are constructed from configured base directory
	receiptFile, err := os.Create(receiptPath)
	if err != nil {
		return fmt.Errorf("failed to create receipt parquet file: %w", err)
	}

	// #nosec G304 -- paths are constructed from configured base directory
	logFile, err := os.Create(logPath)
	if err != nil {
		if closeErr := receiptFile.Close(); closeErr != nil {
			return fmt.Errorf("failed to create log parquet file: %w; close receipt file error: %v", err, closeErr)
		}
		return fmt.Errorf("failed to create log parquet file: %w", err)
	}

	blockNumberSorting := parquet.SortingWriterConfig(
		parquet.SortingColumns(parquet.Ascending("block_number")),
	)

	receiptWriter := parquet.NewGenericWriter[ReceiptRecord](receiptFile,
		parquet.Compression(&parquet.Snappy),
		blockNumberSorting,
	)
	logWriter := parquet.NewGenericWriter[LogRecord](logFile,
		parquet.Compression(&parquet.Snappy),
		blockNumberSorting,
	)

	s.receiptFile = receiptFile
	s.logFile = logFile
	s.receiptWriter = receiptWriter
	s.logWriter = logWriter

	return nil
}

// Flush acquires the write lock and flushes all buffered data to disk.
// Mostly used for testing and benchmarking.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked()
}

func (s *Store) flushLocked() error {
	if len(s.receiptsBuffer) == 0 {
		return nil
	}

	if h := s.FaultHooks; h != nil && h.BeforeFlush != nil {
		if err := h.BeforeFlush(s.lastSeenBlock); err != nil {
			return err
		}
	}

	if _, err := s.receiptWriter.Write(s.receiptsBuffer); err != nil {
		return fmt.Errorf("failed to write receipts to parquet: %w", err)
	}
	if err := s.receiptWriter.Flush(); err != nil {
		return fmt.Errorf("failed to flush receipt parquet writer: %w", err)
	}

	if len(s.logsBuffer) > 0 {
		if _, err := s.logWriter.Write(s.logsBuffer); err != nil {
			return fmt.Errorf("failed to write logs to parquet: %w", err)
		}
		if err := s.logWriter.Flush(); err != nil {
			return fmt.Errorf("failed to flush log parquet writer: %w", err)
		}
	}

	if h := s.FaultHooks; h != nil && h.AfterFlush != nil {
		if err := h.AfterFlush(s.lastSeenBlock); err != nil {
			return err
		}
	}

	s.receiptsBuffer = s.receiptsBuffer[:0]
	s.logsBuffer = s.logsBuffer[:0]
	return nil
}

func (s *Store) closeWritersLocked() error {
	var errs []error

	if s.receiptWriter != nil {
		if err := s.receiptWriter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("receipt writer: %w", err))
		}
	}
	if s.logWriter != nil {
		if err := s.logWriter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("log writer: %w", err))
		}
	}
	if s.receiptFile != nil {
		if err := s.receiptFile.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("receipt file sync: %w", err))
		}
		if err := s.receiptFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("receipt file: %w", err))
		}
	}
	if s.logFile != nil {
		if err := s.logFile.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("log file sync: %w", err))
		}
		if err := s.logFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("log file: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

func int64FromUint64(value uint64) (int64, error) {
	if value > uint64(maxInt64) {
		return 0, fmt.Errorf("value %d overflows int64", value)
	}
	return int64(value), nil
}

// Uint32FromUint safely converts uint to uint32.
func Uint32FromUint(value uint) uint32 {
	if value > uint(maxUint32) {
		return maxUint32
	}
	return uint32(value)
}

// CopyBytes creates a copy of a byte slice.
func CopyBytes(src []byte) []byte {
	if len(src) == 0 {
		return make([]byte, 0)
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// CopyBytesOrEmpty creates a copy of a byte slice, returning empty slice for nil.
func CopyBytesOrEmpty(src []byte) []byte {
	if src == nil {
		return make([]byte, 0)
	}
	return CopyBytes(src)
}
