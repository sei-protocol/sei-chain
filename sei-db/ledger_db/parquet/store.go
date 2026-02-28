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
	dbLogger "github.com/sei-protocol/sei-db/common/logger"
	dbwal "github.com/sei-protocol/sei-db/wal"
)

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
}

// DefaultStoreConfig returns the default store configuration.
func DefaultStoreConfig() StoreConfig {
	return StoreConfig{
		BlockFlushInterval: defaultBlockFlushInterval,
		MaxBlocksPerFile:   defaultMaxBlocksPerFile,
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
	AfterWALWrite    func(blockNumber uint64) error // after WAL writes, before parquet apply
	BeforeFlush      func(blockNumber uint64) error // before writing buffers to parquet
	AfterFlush       func(blockNumber uint64) error // after parquet flush, before buffer clear
	AfterCloseWriters func(blockNumber uint64) error // during rotation, after closing old writers
	AfterWALClear    func(blockNumber uint64) error // during rotation, after WAL truncation
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
	blocksInFile     uint64

	Reader          *Reader
	wal             dbwal.GenericWAL[WALEntry]
	latestVersion   atomic.Int64
	earliestVersion atomic.Int64
	closeOnce       sync.Once

	log       dbLogger.Logger
	pruneStop chan struct{}

	// WarmupRecords holds receipts recovered from WAL for cache warming.
	WarmupRecords []ReceiptRecord

	// FaultHooks is nil in production. Tests can set this to inject faults
	// at specific points in the write path for crash recovery testing.
	FaultHooks *FaultHooks
}

// NewStore creates a new parquet store.
func NewStore(log dbLogger.Logger, cfg StoreConfig) (*Store, error) {
	storeCfg := resolveStoreConfig(cfg)

	if err := os.MkdirAll(cfg.DBDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create parquet base directory: %w", err)
	}

	reader, err := NewReaderWithMaxBlocksPerFile(cfg.DBDirectory, storeCfg.MaxBlocksPerFile)
	if err != nil {
		return nil, err
	}

	walDir := filepath.Join(cfg.DBDirectory, "parquet-wal")
	receiptWAL, err := NewWAL(log, walDir)
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
		log:            log,
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

// GetReceiptByTxHash retrieves a receipt by transaction hash.
func (s *Store) GetReceiptByTxHash(ctx context.Context, txHash common.Hash) (*ReceiptResult, error) {
	return s.Reader.GetReceiptByTxHash(ctx, txHash)
}

// GetLogs retrieves logs matching the filter.
func (s *Store) GetLogs(ctx context.Context, filter LogFilter) ([]LogResult, error) {
	return s.Reader.GetLogs(ctx, filter)
}

// WriteReceipts writes multiple receipts, batching WAL writes per block.
func (s *Store) WriteReceipts(inputs []ReceiptInput) error {
	if len(inputs) == 0 {
		return nil
	}

	// Group receipt bytes by block number for batched WAL writes.
	// Preserve encounter order so WAL entries are written in block order.
	type blockBatch struct {
		blockNumber uint64
		receipts    [][]byte
	}
	var batches []blockBatch
	batchIdx := make(map[uint64]int)

	for i := range inputs {
		bn := inputs[i].BlockNumber
		if idx, ok := batchIdx[bn]; ok {
			batches[idx].receipts = append(batches[idx].receipts, inputs[i].ReceiptBytes)
		} else {
			batchIdx[bn] = len(batches)
			batches = append(batches, blockBatch{
				blockNumber: bn,
				receipts:    [][]byte{inputs[i].ReceiptBytes},
			})
		}
	}

	// Write one WAL entry per block
	for _, b := range batches {
		entry := WALEntry{
			BlockNumber: b.blockNumber,
			Receipts:    b.receipts,
		}
		if err := s.wal.Write(entry); err != nil {
			return err
		}
	}

	if h := s.FaultHooks; h != nil && h.AfterWALWrite != nil {
		if err := h.AfterWALWrite(inputs[0].BlockNumber); err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range inputs {
		if err := s.applyReceiptLocked(inputs[i]); err != nil {
			return err
		}
	}

	return nil
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
		}
	})

	return err
}

// WAL returns the WAL for replay purposes.
func (s *Store) WAL() dbwal.GenericWAL[WALEntry] {
	return s.wal
}

// ApplyReceiptFromReplay applies a receipt during WAL replay.
func (s *Store) ApplyReceiptFromReplay(input ReceiptInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyReceiptLocked(input)
}

// FileStartBlock returns the current file start block.
func (s *Store) FileStartBlock() uint64 {
	return s.fileStartBlock
}

// ClearWAL truncates the WAL after a successful file rotation.
func (s *Store) ClearWAL() error {
	firstOffset, errFirst := s.wal.FirstOffset()
	if errFirst != nil || firstOffset <= 0 {
		return nil
	}
	lastOffset, errLast := s.wal.LastOffset()
	if errLast != nil || lastOffset <= 0 {
		return nil
	}
	if lastOffset < firstOffset {
		return nil
	}
	if err := s.wal.TruncateBefore(lastOffset + 1); err != nil {
		if strings.Contains(err.Error(), "out of range") {
			return nil
		}
		return fmt.Errorf("failed to truncate parquet WAL before offset %d: %w", lastOffset+1, err)
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
				pruned := s.pruneOldFiles(uint64(pruneBeforeBlock))
				if pruned > 0 && s.log != nil {
					s.log.Info(fmt.Sprintf("Pruned %d parquet file pairs older than block %d", pruned, pruneBeforeBlock))
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

func (s *Store) pruneOldFiles(pruneBeforeBlock uint64) int {
	// Get list of files to prune from the reader
	filesToPrune := s.Reader.GetFilesBeforeBlock(pruneBeforeBlock)
	if len(filesToPrune) == 0 {
		return 0
	}

	prunedCount := 0
	for _, filePair := range filesToPrune {
		receiptRemoved := filePair.ReceiptFile == ""
		if filePair.ReceiptFile != "" {
			s.Reader.RemoveTrackedReceiptFile(filePair.StartBlock)
			if err := removeFile(filePair.ReceiptFile); err != nil && !os.IsNotExist(err) {
				s.Reader.AddTrackedReceiptFile(filePair.StartBlock)
				if s.log != nil {
					s.log.Error("failed to prune receipt file", "file", filePair.ReceiptFile, "err", err)
				}
			} else {
				receiptRemoved = true
			}
		}

		logRemoved := filePair.LogFile == ""
		if filePair.LogFile != "" {
			s.Reader.RemoveTrackedLogFile(filePair.StartBlock)
			if err := removeFile(filePair.LogFile); err != nil && !os.IsNotExist(err) {
				s.Reader.AddTrackedLogFile(filePair.StartBlock)
				if s.log != nil {
					s.log.Error("failed to prune log file", "file", filePair.LogFile, "err", err)
				}
			} else {
				logRemoved = true
			}
		}

		if receiptRemoved && logRemoved {
			prunedCount++
		}
	}

	return prunedCount
}

func (s *Store) applyReceiptLocked(input ReceiptInput) error {
	// Lazy writer initialization: defer file creation until the first receipt
	// arrives so the parquet filename reflects the actual starting block number
	// (e.g. receipts_195360501.parquet) rather than a misleading receipts_0.parquet.
	if s.receiptWriter == nil {
		s.fileStartBlock = input.BlockNumber
		if err := s.initWriters(); err != nil {
			return err
		}
	}

	blockNumber := input.BlockNumber
	isNewBlock := blockNumber != s.lastSeenBlock
	if isNewBlock {
		if s.lastSeenBlock != 0 {
			s.blocksSinceFlush++
			s.blocksInFile++
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

	if isNewBlock && s.shouldRotateFile() {
		if err := s.rotateFileLocked(blockNumber); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) shouldRotateFile() bool {
	if s.config.MaxBlocksPerFile > 0 && s.blocksInFile >= s.config.MaxBlocksPerFile {
		return true
	}
	return false
}

func (s *Store) rotateFileLocked(newBlockNumber uint64) error {
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
	if err := s.ClearWAL(); err != nil {
		return err
	}

	if h := s.FaultHooks; h != nil && h.AfterWALClear != nil {
		if err := h.AfterWALClear(newBlockNumber); err != nil {
			return err
		}
	}

	s.fileStartBlock = newBlockNumber
	s.blocksInFile = 0

	return s.initWriters()
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
