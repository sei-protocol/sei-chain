package parquet_v2

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
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

var logger = seilog.NewLogger("db", "ledger-db", "parquet-v2")

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
// All fields are nil in production. When non-nil, the hook is called at the
// corresponding point in the write path; returning a non-nil error aborts the
// operation and propagates the error to the caller.
type FaultHooks struct {
	AfterWALWrite     func(blockNumber uint64) error
	BeforeFlush       func(blockNumber uint64) error
	AfterFlush        func(blockNumber uint64) error
	AfterCloseWriters func(blockNumber uint64) error
	AfterWALClear     func(blockNumber uint64) error
}

// ----------------------------------------------------------------------------
// Coordinator request types
// ----------------------------------------------------------------------------

// All public methods on Store enqueue a request struct onto the coordinator
// channel and block on a response channel until the coordinator processes it.
// The coordinator goroutine is the *sole* owner of mutable metadata; no
// internal mutex is required for that state.
//
// In this first pass the coordinator executes I/O synchronously. The shape is
// deliberately request/response so that future commits can replace the
// synchronous body of each `handle*` helper with a delegation to a read or
// write worker without changing the public API.

type writeRequest struct {
	inputs []ReceiptInput
	resp   chan error
}

type applyReplayRequest struct {
	input ReceiptInput
	resp  chan error
}

type observeEmptyBlockRequest struct {
	height uint64
	resp   chan error
}

type getReceiptRequest struct {
	ctx    context.Context
	txHash common.Hash
	// blockNumber == 0 means "no hint, full scan".
	blockNumber uint64
	resp        chan getReceiptResponse
}

type getReceiptResponse struct {
	result *ReceiptResult
	err    error
}

type getLogsRequest struct {
	ctx    context.Context
	filter LogFilter
	resp   chan getLogsResponse
}

type getLogsResponse struct {
	results []LogResult
	err     error
}

type maxReceiptBlockRequest struct {
	ctx  context.Context
	resp chan maxReceiptBlockResponse
}

type maxReceiptBlockResponse struct {
	max uint64
	ok  bool
	err error
}

type flushRequest struct {
	resp chan error
}

type clearWALRequest struct {
	resp chan error
}

type pruneRequest struct {
	pruneBeforeBlock uint64
	resp             chan int
}

type closeRequest struct {
	resp chan error
}

// crashRequest skips the graceful flush + writer close path. It only releases
// raw file descriptors (so the test process can reopen the directory) and
// closes the WAL/reader. Used to simulate kill -9 or power-loss scenarios.
type crashRequest struct {
	resp chan struct{}
}

type setBlockFlushIntervalRequest struct {
	interval uint64
	resp     chan struct{}
}

type setMaxBlocksPerFileRequest struct {
	maxBlocksPerFile uint64
	resp             chan struct{}
}

type setFaultHooksRequest struct {
	hooks *FaultHooks
	resp  chan struct{}
}

// getMaxBlocksPerFileRequest reads coordinator-owned config back to callers.
// A non-nil fileStart channel selects "fileStartBlock" instead of
// "maxBlocksPerFile" for the response — both are tiny lookups so they share
// one handler.
type getMaxBlocksPerFileRequest struct {
	resp      chan uint64
	fileStart chan uint64
}

type getKeepRecentRequest struct {
	resp chan int64
}

// coordinatorRequest is the union type sent on the coordinator channel.
// Exactly one field is non-nil.
type coordinatorRequest struct {
	write                 *writeRequest
	applyReplay           *applyReplayRequest
	observeEmptyBlock     *observeEmptyBlockRequest
	getReceipt            *getReceiptRequest
	getLogs               *getLogsRequest
	maxReceiptBlock       *maxReceiptBlockRequest
	flush                 *flushRequest
	clearWAL              *clearWALRequest
	prune                 *pruneRequest
	close                 *closeRequest
	setBlockFlushInterval *setBlockFlushIntervalRequest
	setMaxBlocksPerFile   *setMaxBlocksPerFileRequest
	setFaultHooks         *setFaultHooksRequest
	getMaxBlocksPerFile   *getMaxBlocksPerFileRequest
	getKeepRecent         *getKeepRecentRequest
	crash                 *crashRequest
}

// ----------------------------------------------------------------------------
// Store
// ----------------------------------------------------------------------------

// Store is the parquet-based receipt store, V2.
//
// Public methods are thin: they construct a request, send it to the coordinator
// goroutine on `requests`, and block on a per-request response channel.
//
// The coordinator goroutine owns all mutable metadata (the coordinator state
// struct). Reads of `latestVersion` / `earliestVersion` happen on hot paths
// from many callers, so those are kept as atomics — they are otherwise
// independent from coordinator-owned metadata.
type Store struct {
	requests chan coordinatorRequest
	done     chan struct{}
	loopDone chan struct{}

	closeOnce sync.Once

	// Atomic counters: read by callers without going through the coordinator
	// because they are independent of coordinator-owned metadata.
	latestVersion   atomic.Int64
	earliestVersion atomic.Int64

	// WarmupRecords holds receipts recovered from WAL for cache warming.
	// Set during initialization (before coordinator starts) and read once by
	// the cache layer; not mutated after Open returns.
	WarmupRecords []ReceiptRecord

	// Set during Open() and never published again. Public methods use this
	// only to expose the WAL handle to the higher-level wrapper for replay.
	// The coordinator goroutine owns the equivalent reference in
	// coordinatorState.wal for write/clear/close paths.
	wal dbwal.GenericWAL[WALEntry]

	// pruneStop is closed by Close() to terminate the background prune
	// goroutine. The prune goroutine sends pruneRequest into the coordinator;
	// it owns no metadata itself.
	pruneStop chan struct{}
	pruneWg   sync.WaitGroup
}

// coordinatorState holds all metadata mutated by the coordinator goroutine.
// Only the coordinator loop touches this struct after Open; no mutex required.
type coordinatorState struct {
	cfg StoreConfig

	// Active writers / files for the currently open parquet file.
	receiptWriter *parquet.GenericWriter[ReceiptRecord]
	logWriter     *parquet.GenericWriter[LogRecord]
	receiptFile   *os.File
	logFile       *os.File

	fileStartBlock   uint64
	receiptsBuffer   []ReceiptRecord
	logsBuffer       []LogRecord
	lastSeenBlock    uint64
	blocksSinceFlush uint64

	// Tracked closed parquet files (immutable, available for reads).
	// Sorted by start block.
	closedReceiptFiles []string
	closedLogFiles     []string

	// Reader is a stateless DuckDB query helper.
	reader *Reader

	// WAL handle, owned by the coordinator state for read/write/clear paths.
	wal dbwal.GenericWAL[WALEntry]

	// Test-only: fault injection points.
	faultHooks *FaultHooks
}

// NewStore creates a new V2 parquet store. It performs synchronous startup
// (file scan, WAL recovery is handled by the higher-level wrapper which then
// routes through ApplyReceiptFromReplay). After NewStore returns the
// coordinator goroutine is running and the public API is ready.
func NewStore(cfg StoreConfig) (*Store, error) {
	storeCfg := resolveStoreConfig(cfg)

	if err := os.MkdirAll(storeCfg.DBDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create parquet base directory: %w", err)
	}

	reader, err := NewReader()
	if err != nil {
		return nil, err
	}

	receiptFiles, logFiles := reader.ScanExistingFiles(storeCfg.DBDirectory)

	walDir := filepath.Join(storeCfg.DBDirectory, "parquet-wal")
	receiptWAL, err := NewWAL(walDir)
	if err != nil {
		_ = reader.Close()
		return nil, err
	}

	st := &coordinatorState{
		cfg:                storeCfg,
		receiptsBuffer:     make([]ReceiptRecord, 0, 1000),
		logsBuffer:         make([]LogRecord, 0, 10000),
		closedReceiptFiles: receiptFiles,
		closedLogFiles:     logFiles,
		reader:             reader,
		wal:                receiptWAL,
	}

	store := &Store{
		requests:  make(chan coordinatorRequest),
		done:      make(chan struct{}),
		loopDone:  make(chan struct{}),
		wal:       receiptWAL,
		pruneStop: make(chan struct{}),
	}

	// Initialize latest version from disk before starting the coordinator so
	// the prune goroutine and other readers see the recovered value.
	if maxBlock, ok, err := reader.MaxReceiptBlockNumber(context.Background(), receiptFiles); err != nil {
		_ = reader.Close()
		_ = receiptWAL.Close()
		return nil, err
	} else if ok {
		latest, err := int64FromUint64(maxBlock)
		if err != nil {
			_ = reader.Close()
			_ = receiptWAL.Close()
			return nil, err
		}
		store.latestVersion.Store(latest)
		if maxBlock < ^uint64(0) {
			st.fileStartBlock = maxBlock + 1
		}
	}

	go store.coordinatorLoop(st)
	store.startPruning(storeCfg.PruneIntervalSeconds)

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

// ----------------------------------------------------------------------------
// Public API: every method enqueues a request and blocks on a response.
// ----------------------------------------------------------------------------

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

// UpdateLatestVersion updates the latest version if the new value is higher.
func (s *Store) UpdateLatestVersion(version int64) {
	for {
		current := s.latestVersion.Load()
		if version <= current {
			return
		}
		if s.latestVersion.CompareAndSwap(current, version) {
			return
		}
	}
}

// CacheRotateInterval returns the interval at which the cache should rotate.
// Read from immutable config; safe without coordinator.
func (s *Store) CacheRotateInterval() uint64 {
	// Config values can be mutated via Set* helpers (test-only), but the
	// rotate interval is read on hot paths and is not safety-critical for
	// correctness. We forward through the coordinator on the test setters
	// instead, and read it lock-free here.
	resp := make(chan uint64, 1)
	if !s.send(coordinatorRequest{getMaxBlocksPerFile: &getMaxBlocksPerFileRequest{resp: resp}}) {
		return 0
	}
	return <-resp
}

// SetBlockFlushInterval overrides the number of blocks buffered before
// flushing to a parquet file. Intended for testing.
func (s *Store) SetBlockFlushInterval(interval uint64) {
	resp := make(chan struct{}, 1)
	if !s.send(coordinatorRequest{setBlockFlushInterval: &setBlockFlushIntervalRequest{interval: interval, resp: resp}}) {
		return
	}
	<-resp
}

// SetMaxBlocksPerFile overrides the rotation interval after construction.
// Intended for tests. Not safe to call while writes are in flight.
func (s *Store) SetMaxBlocksPerFile(n uint64) {
	resp := make(chan struct{}, 1)
	if !s.send(coordinatorRequest{setMaxBlocksPerFile: &setMaxBlocksPerFileRequest{maxBlocksPerFile: n, resp: resp}}) {
		return
	}
	<-resp
}

// SetFaultHooks installs fault hooks for testing crash recovery.
func (s *Store) SetFaultHooks(hooks *FaultHooks) {
	resp := make(chan struct{}, 1)
	if !s.send(coordinatorRequest{setFaultHooks: &setFaultHooksRequest{hooks: hooks, resp: resp}}) {
		return
	}
	<-resp
}

// GetReceiptByTxHash retrieves a receipt by transaction hash via a full scan
// of the closed parquet files.
func (s *Store) GetReceiptByTxHash(ctx context.Context, txHash common.Hash) (*ReceiptResult, error) {
	resp := make(chan getReceiptResponse, 1)
	if !s.send(coordinatorRequest{getReceipt: &getReceiptRequest{ctx: ctx, txHash: txHash, resp: resp}}) {
		return nil, errStoreClosed
	}
	r := <-resp
	return r.result, r.err
}

// GetReceiptByTxHashInBlock narrows the parquet search to the file containing
// blockNumber, falling back to a full scan on miss.
func (s *Store) GetReceiptByTxHashInBlock(ctx context.Context, txHash common.Hash, blockNumber uint64) (*ReceiptResult, error) {
	resp := make(chan getReceiptResponse, 1)
	if !s.send(coordinatorRequest{getReceipt: &getReceiptRequest{ctx: ctx, txHash: txHash, blockNumber: blockNumber, resp: resp}}) {
		return nil, errStoreClosed
	}
	r := <-resp
	return r.result, r.err
}

// GetLogs retrieves logs matching the filter.
func (s *Store) GetLogs(ctx context.Context, filter LogFilter) ([]LogResult, error) {
	resp := make(chan getLogsResponse, 1)
	if !s.send(coordinatorRequest{getLogs: &getLogsRequest{ctx: ctx, filter: filter, resp: resp}}) {
		return nil, errStoreClosed
	}
	r := <-resp
	return r.results, r.err
}

// WriteReceipts writes multiple receipts. Rotation fires on aligned block
// boundaries (blockNumber % MaxBlocksPerFile == 0).
func (s *Store) WriteReceipts(inputs []ReceiptInput) error {
	if len(inputs) == 0 {
		return nil
	}
	resp := make(chan error, 1)
	if !s.send(coordinatorRequest{write: &writeRequest{inputs: inputs, resp: resp}}) {
		return errStoreClosed
	}
	return <-resp
}

// ApplyReceiptFromReplay applies a receipt during WAL replay. If the block
// number is on a rotation boundary, this rotates the file (without touching
// the WAL) so replay-recovered blocks land in the same aligned files the
// write path would have produced.
func (s *Store) ApplyReceiptFromReplay(input ReceiptInput) error {
	resp := make(chan error, 1)
	if !s.send(coordinatorRequest{applyReplay: &applyReplayRequest{input: input, resp: resp}}) {
		return errStoreClosed
	}
	return <-resp
}

// ObserveEmptyBlock signals that a block with no receipts was committed at
// height. Without this, a boundary-aligned empty block would leave the open
// file accepting writes past MaxBlocksPerFile.
func (s *Store) ObserveEmptyBlock(height uint64) error {
	resp := make(chan error, 1)
	if !s.send(coordinatorRequest{observeEmptyBlock: &observeEmptyBlockRequest{height: height, resp: resp}}) {
		return errStoreClosed
	}
	return <-resp
}

// Flush forces all buffered data to disk.
func (s *Store) Flush() error {
	resp := make(chan error, 1)
	if !s.send(coordinatorRequest{flush: &flushRequest{resp: resp}}) {
		return errStoreClosed
	}
	return <-resp
}

// ClearWAL truncates the WAL after rotation, preserving the last entry.
// Exposed because the higher-level wrapper invokes it during replay cleanup.
func (s *Store) ClearWAL() error {
	resp := make(chan error, 1)
	if !s.send(coordinatorRequest{clearWAL: &clearWALRequest{resp: resp}}) {
		return errStoreClosed
	}
	return <-resp
}

// FileStartBlock returns the current file start block.
func (s *Store) FileStartBlock() uint64 {
	resp := make(chan uint64, 1)
	if !s.send(coordinatorRequest{getMaxBlocksPerFile: &getMaxBlocksPerFileRequest{resp: nil, fileStart: resp}}) {
		return 0
	}
	return <-resp
}

// IsRotationBoundary returns true when blockNumber is aligned to the file
// rotation interval.
func (s *Store) IsRotationBoundary(blockNumber uint64) bool {
	resp := make(chan uint64, 1)
	if !s.send(coordinatorRequest{getMaxBlocksPerFile: &getMaxBlocksPerFileRequest{resp: resp}}) {
		return false
	}
	mbpf := <-resp
	if mbpf == 0 {
		return false
	}
	return blockNumber%mbpf == 0
}

// PruneOldFiles removes parquet file pairs whose data is entirely before
// pruneBeforeBlock. Returns the number of file pairs removed.
func (s *Store) PruneOldFiles(pruneBeforeBlock uint64) int {
	resp := make(chan int, 1)
	if !s.send(coordinatorRequest{prune: &pruneRequest{pruneBeforeBlock: pruneBeforeBlock, resp: resp}}) {
		return 0
	}
	return <-resp
}

// WAL returns the WAL handle. Used for replay by the higher-level wrapper.
func (s *Store) WAL() dbwal.GenericWAL[WALEntry] {
	return s.wal
}

// SimulateCrash abandons the store without flushing or finalizing, mimicking
// abrupt process termination. Releases file descriptors only so the test
// process can reopen the directory; in-memory buffers and parquet footers are
// deliberately abandoned.
func (s *Store) SimulateCrash() {
	s.closeOnce.Do(func() {
		close(s.pruneStop)
		s.pruneWg.Wait()

		resp := make(chan struct{}, 1)
		if s.send(coordinatorRequest{crash: &crashRequest{resp: resp}}) {
			<-resp
		}
		close(s.done)
		<-s.loopDone
	})
}

// Close flushes outstanding state, closes the active parquet files and writers,
// shuts down the WAL, and closes the reader.
func (s *Store) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.pruneStop)
		s.pruneWg.Wait()

		resp := make(chan error, 1)
		// Send a graceful close request through the coordinator. Even if the
		// loop has already exited (e.g. simulated crash), we don't deadlock
		// because send() falls back to nil/false when done is closed.
		if s.send(coordinatorRequest{close: &closeRequest{resp: resp}}) {
			err = <-resp
		}
		close(s.done)
		<-s.loopDone
	})
	return err
}

// errStoreClosed is returned when the coordinator has stopped.
var errStoreClosed = fmt.Errorf("parquet_v2 store is closed")

// send pushes a request onto the coordinator channel, returning false if the
// store is shutting down.
func (s *Store) send(req coordinatorRequest) bool {
	select {
	case <-s.done:
		return false
	default:
	}
	select {
	case s.requests <- req:
		return true
	case <-s.done:
		return false
	}
}

// ----------------------------------------------------------------------------
// Coordinator loop and handlers
// ----------------------------------------------------------------------------

// coordinatorLoop is the single goroutine that owns mutable receipt-store
// metadata. All public API methods route through this loop. The handlers run
// I/O synchronously today; later we can replace each handler body with a
// delegation to a read or write worker.
func (s *Store) coordinatorLoop(st *coordinatorState) {
	defer close(s.loopDone)

	for {
		select {
		case <-s.done:
			return
		case req := <-s.requests:
			s.dispatch(st, req)
		}
	}
}

func (s *Store) dispatch(st *coordinatorState, req coordinatorRequest) {
	switch {
	case req.write != nil:
		req.write.resp <- s.handleWrite(st, req.write.inputs)
	case req.applyReplay != nil:
		req.applyReplay.resp <- s.handleApplyReplay(st, req.applyReplay.input)
	case req.observeEmptyBlock != nil:
		req.observeEmptyBlock.resp <- s.handleObserveEmptyBlock(st, req.observeEmptyBlock.height)
	case req.getReceipt != nil:
		result, err := s.handleGetReceipt(st, req.getReceipt.ctx, req.getReceipt.txHash, req.getReceipt.blockNumber)
		req.getReceipt.resp <- getReceiptResponse{result: result, err: err}
	case req.getLogs != nil:
		results, err := s.handleGetLogs(st, req.getLogs.ctx, req.getLogs.filter)
		req.getLogs.resp <- getLogsResponse{results: results, err: err}
	case req.maxReceiptBlock != nil:
		max, ok, err := s.handleMaxReceiptBlock(st, req.maxReceiptBlock.ctx)
		req.maxReceiptBlock.resp <- maxReceiptBlockResponse{max: max, ok: ok, err: err}
	case req.flush != nil:
		req.flush.resp <- s.handleFlush(st)
	case req.clearWAL != nil:
		req.clearWAL.resp <- s.handleClearWAL(st)
	case req.prune != nil:
		req.prune.resp <- s.handlePrune(st, req.prune.pruneBeforeBlock)
	case req.close != nil:
		req.close.resp <- s.handleClose(st)
	case req.setBlockFlushInterval != nil:
		st.cfg.BlockFlushInterval = req.setBlockFlushInterval.interval
		req.setBlockFlushInterval.resp <- struct{}{}
	case req.setMaxBlocksPerFile != nil:
		st.cfg.MaxBlocksPerFile = req.setMaxBlocksPerFile.maxBlocksPerFile
		req.setMaxBlocksPerFile.resp <- struct{}{}
	case req.setFaultHooks != nil:
		st.faultHooks = req.setFaultHooks.hooks
		req.setFaultHooks.resp <- struct{}{}
	case req.getMaxBlocksPerFile != nil:
		// Two response shapes share this request type: maxBlocksPerFile and
		// fileStartBlock. The non-nil channel selects which one to return.
		if req.getMaxBlocksPerFile.fileStart != nil {
			req.getMaxBlocksPerFile.fileStart <- st.fileStartBlock
		} else {
			req.getMaxBlocksPerFile.resp <- st.cfg.MaxBlocksPerFile
		}
	case req.getKeepRecent != nil:
		req.getKeepRecent.resp <- st.cfg.KeepRecent
	case req.crash != nil:
		s.handleCrash(st)
		req.crash.resp <- struct{}{}
	}
}

// handleWrite runs the equivalent of v1 WriteReceipts inside the coordinator.
//
// Rotation fires on aligned block boundaries. WAL.Write must run before
// rotateFile: rotation clears the WAL, so a crash between clear and a later
// WAL write would lose the boundary block. The coordinator preserves that
// invariant trivially because it processes one batch at a time.
func (s *Store) handleWrite(st *coordinatorState, inputs []ReceiptInput) error {
	if len(inputs) == 0 {
		return nil
	}

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

	for _, b := range batches {
		entry := WALEntry{
			BlockNumber: b.blockNumber,
			Receipts:    b.receipts,
		}
		if err := st.wal.Write(entry); err != nil {
			return err
		}

		if h := st.faultHooks; h != nil && h.AfterWALWrite != nil {
			if err := h.AfterWALWrite(b.blockNumber); err != nil {
				return err
			}
		}

		if st.receiptWriter != nil && b.blockNumber != st.lastSeenBlock && isRotationBoundary(b.blockNumber, st.cfg.MaxBlocksPerFile) {
			if err := s.rotateFile(st, b.blockNumber, true); err != nil {
				return err
			}
		}

		for i := range b.inputs {
			if err := s.applyReceipt(st, b.inputs[i]); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Store) handleApplyReplay(st *coordinatorState, input ReceiptInput) error {
	if st.receiptWriter != nil && input.BlockNumber != st.lastSeenBlock && isRotationBoundary(input.BlockNumber, st.cfg.MaxBlocksPerFile) {
		if err := s.rotateFile(st, input.BlockNumber, false); err != nil {
			return err
		}
	}
	return s.applyReceipt(st, input)
}

func (s *Store) handleObserveEmptyBlock(st *coordinatorState, height uint64) error {
	if height <= st.lastSeenBlock {
		return nil
	}
	if st.receiptWriter == nil || !isRotationBoundary(height, st.cfg.MaxBlocksPerFile) {
		st.lastSeenBlock = height
		return nil
	}
	if err := s.rotateFile(st, height, true); err != nil {
		return err
	}
	st.lastSeenBlock = height
	return nil
}

func (s *Store) handleGetReceipt(st *coordinatorState, ctx context.Context, txHash common.Hash, blockHint uint64) (*ReceiptResult, error) {
	files := snapshot(st.closedReceiptFiles)
	if blockHint > 0 {
		// Narrow to the candidate file first.
		candidate := FileForBlock(files, blockHint)
		if candidate != "" {
			result, err := st.reader.GetReceiptByTxHash(ctx, txHash, []string{candidate})
			if err != nil {
				return nil, err
			}
			if result != nil {
				return result, nil
			}
		}
	}
	return st.reader.GetReceiptByTxHash(ctx, txHash, files)
}

func (s *Store) handleGetLogs(st *coordinatorState, ctx context.Context, filter LogFilter) ([]LogResult, error) {
	files := FilesForLogQuery(st.closedLogFiles, st.cfg.MaxBlocksPerFile, filter)
	return st.reader.GetLogs(ctx, files, filter)
}

func (s *Store) handleMaxReceiptBlock(st *coordinatorState, ctx context.Context) (uint64, bool, error) {
	files := snapshot(st.closedReceiptFiles)
	return st.reader.MaxReceiptBlockNumber(ctx, files)
}

func (s *Store) handleFlush(st *coordinatorState) error {
	return s.flush(st)
}

func (s *Store) handleClearWAL(st *coordinatorState) error {
	return clearWAL(st.wal)
}

func (s *Store) handlePrune(st *coordinatorState, pruneBeforeBlock uint64) int {
	files := FilesBeforeBlock(st.closedReceiptFiles, st.cfg.DBDirectory, st.cfg.MaxBlocksPerFile, pruneBeforeBlock)
	if len(files) == 0 {
		return 0
	}
	prunedCount := 0
	for _, fp := range files {
		// In v1 this is two phases (remove from tracking, wait for in-flight
		// readers via pruneMu, delete) because reads can happen concurrently
		// with the pruner. In V2 the coordinator is single-threaded and reads
		// happen inside this same goroutine, so removing from the closed-list
		// snapshot and deleting the file is atomic from the coordinator's
		// point of view.
		//
		// TODO(coordinator-readers): when read workers come online, the
		// coordinator must reference-count in-flight reads on each file before
		// physical delete. Track this via a coordinator-owned
		// map[startBlock]int and only call removeFile after the count drops
		// to zero. No mutex needed since only the coordinator mutates the
		// map.
		removeFromClosed(&st.closedReceiptFiles, fp.StartBlock)
		removeFromClosed(&st.closedLogFiles, fp.StartBlock)

		receiptRemoved := fp.ReceiptFile == ""
		if fp.ReceiptFile != "" {
			if err := removeFile(fp.ReceiptFile); err != nil && !os.IsNotExist(err) {
				logger.Error("failed to prune receipt file", "file", fp.ReceiptFile, "err", err)
			} else {
				receiptRemoved = true
			}
		}
		logRemoved := fp.LogFile == ""
		if fp.LogFile != "" {
			if err := removeFile(fp.LogFile); err != nil && !os.IsNotExist(err) {
				logger.Error("failed to prune log file", "file", fp.LogFile, "err", err)
			} else {
				logRemoved = true
			}
		}
		// On failed deletion, restore tracking so future scans/queries still
		// see the file. Coordinator reads only its own state, no race.
		if !receiptRemoved && fp.ReceiptFile != "" {
			addToClosedSorted(&st.closedReceiptFiles, fp.ReceiptFile)
		}
		if !logRemoved && fp.LogFile != "" {
			addToClosedSorted(&st.closedLogFiles, fp.LogFile)
		}

		if receiptRemoved && logRemoved {
			prunedCount++
		}
	}
	return prunedCount
}

// handleCrash mirrors v1 SimulateCrash: skip flush, skip writer Close (which
// would write the parquet footer), only release raw file descriptors and
// close WAL/reader. The on-disk parquet file is intentionally left without a
// footer so reopen exercises the corrupt-file recovery path.
func (s *Store) handleCrash(st *coordinatorState) {
	if st.receiptFile != nil {
		_ = st.receiptFile.Close()
		st.receiptFile = nil
	}
	if st.logFile != nil {
		_ = st.logFile.Close()
		st.logFile = nil
	}
	st.receiptWriter = nil
	st.logWriter = nil
	if st.wal != nil {
		_ = st.wal.Close()
		st.wal = nil
	}
	if st.reader != nil {
		_ = st.reader.Close()
		st.reader = nil
	}
}

func (s *Store) handleClose(st *coordinatorState) error {
	if err := s.flush(st); err != nil {
		return err
	}
	if err := closeWriters(st); err != nil {
		return err
	}
	if st.wal != nil {
		if err := st.wal.Close(); err != nil {
			return err
		}
		st.wal = nil
	}
	if st.reader != nil {
		if err := st.reader.Close(); err != nil {
			return err
		}
		st.reader = nil
	}
	return nil
}

// ----------------------------------------------------------------------------
// Coordinator-internal helpers (called only from coordinator goroutine)
// ----------------------------------------------------------------------------

func (s *Store) applyReceipt(st *coordinatorState, input ReceiptInput) error {
	if st.receiptWriter == nil {
		aligned := alignedFileStartBlock(input.BlockNumber, st.cfg.MaxBlocksPerFile)
		if aligned >= st.fileStartBlock {
			st.fileStartBlock = aligned
		}
		if err := initWriters(st); err != nil {
			return err
		}
	}

	blockNumber := input.BlockNumber
	if blockNumber != st.lastSeenBlock {
		if st.lastSeenBlock != 0 {
			st.blocksSinceFlush++
		}
		st.lastSeenBlock = blockNumber
	}

	st.receiptsBuffer = append(st.receiptsBuffer, input.Receipt)
	if len(input.Logs) > 0 {
		st.logsBuffer = append(st.logsBuffer, input.Logs...)
	}

	if st.cfg.BlockFlushInterval > 0 && st.blocksSinceFlush >= st.cfg.BlockFlushInterval {
		if err := s.flush(st); err != nil {
			return err
		}
		st.blocksSinceFlush = 0
	}

	return nil
}

// rotateFile closes the current parquet file and opens a new one at
// newBlockNumber. If clearWALAfter is true the WAL is truncated (preserving
// the last entry); during replay we skip truncation because the outer scan
// would break.
func (s *Store) rotateFile(st *coordinatorState, newBlockNumber uint64, clearWALAfter bool) error {
	if err := s.flush(st); err != nil {
		return err
	}

	oldStartBlock := st.fileStartBlock
	if err := closeWriters(st); err != nil {
		return err
	}

	if h := st.faultHooks; h != nil && h.AfterCloseWriters != nil {
		if err := h.AfterCloseWriters(newBlockNumber); err != nil {
			return err
		}
	}

	addToClosedSorted(&st.closedReceiptFiles, filepath.Join(st.cfg.DBDirectory, fmt.Sprintf("receipts_%d.parquet", oldStartBlock)))
	addToClosedSorted(&st.closedLogFiles, filepath.Join(st.cfg.DBDirectory, fmt.Sprintf("logs_%d.parquet", oldStartBlock)))

	st.fileStartBlock = newBlockNumber

	if err := initWriters(st); err != nil {
		return err
	}

	st.blocksSinceFlush = 0

	if clearWALAfter {
		if err := clearWAL(st.wal); err != nil {
			return err
		}
		if h := st.faultHooks; h != nil && h.AfterWALClear != nil {
			if err := h.AfterWALClear(newBlockNumber); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Store) flush(st *coordinatorState) error {
	if len(st.receiptsBuffer) == 0 {
		return nil
	}

	if h := st.faultHooks; h != nil && h.BeforeFlush != nil {
		if err := h.BeforeFlush(st.lastSeenBlock); err != nil {
			return err
		}
	}

	if _, err := st.receiptWriter.Write(st.receiptsBuffer); err != nil {
		return fmt.Errorf("failed to write receipts to parquet: %w", err)
	}
	if err := st.receiptWriter.Flush(); err != nil {
		return fmt.Errorf("failed to flush receipt parquet writer: %w", err)
	}

	if len(st.logsBuffer) > 0 {
		if _, err := st.logWriter.Write(st.logsBuffer); err != nil {
			return fmt.Errorf("failed to write logs to parquet: %w", err)
		}
		if err := st.logWriter.Flush(); err != nil {
			return fmt.Errorf("failed to flush log parquet writer: %w", err)
		}
	}

	if h := st.faultHooks; h != nil && h.AfterFlush != nil {
		if err := h.AfterFlush(st.lastSeenBlock); err != nil {
			return err
		}
	}

	st.receiptsBuffer = st.receiptsBuffer[:0]
	st.logsBuffer = st.logsBuffer[:0]
	return nil
}

func initWriters(st *coordinatorState) error {
	receiptPath := filepath.Join(st.cfg.DBDirectory, fmt.Sprintf("receipts_%d.parquet", st.fileStartBlock))
	logPath := filepath.Join(st.cfg.DBDirectory, fmt.Sprintf("logs_%d.parquet", st.fileStartBlock))

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

	st.receiptFile = receiptFile
	st.logFile = logFile
	st.receiptWriter = receiptWriter
	st.logWriter = logWriter

	return nil
}

func closeWriters(st *coordinatorState) error {
	var errs []error

	if st.receiptWriter != nil {
		if err := st.receiptWriter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("receipt writer: %w", err))
		}
		st.receiptWriter = nil
	}
	if st.logWriter != nil {
		if err := st.logWriter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("log writer: %w", err))
		}
		st.logWriter = nil
	}
	if st.receiptFile != nil {
		if err := st.receiptFile.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("receipt file sync: %w", err))
		}
		if err := st.receiptFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("receipt file: %w", err))
		}
		st.receiptFile = nil
	}
	if st.logFile != nil {
		if err := st.logFile.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("log file sync: %w", err))
		}
		if err := st.logFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("log file: %w", err))
		}
		st.logFile = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

func clearWAL(w dbwal.GenericWAL[WALEntry]) error {
	if w == nil {
		return nil
	}
	firstOffset, errFirst := w.FirstOffset()
	if errFirst != nil || firstOffset <= 0 {
		return nil
	}
	lastOffset, errLast := w.LastOffset()
	if errLast != nil || lastOffset <= 0 {
		return nil
	}
	if lastOffset <= firstOffset {
		return nil
	}
	if err := w.TruncateBefore(lastOffset); err != nil {
		if strings.Contains(err.Error(), "out of range") {
			return nil
		}
		return fmt.Errorf("failed to truncate parquet WAL before offset %d: %w", lastOffset, err)
	}
	return nil
}

// ----------------------------------------------------------------------------
// Pruning goroutine
// ----------------------------------------------------------------------------

func (s *Store) startPruning(pruneIntervalSeconds int64) {
	// Read keep-recent via a tiny coordinator query rather than caching it on
	// the Store struct. The pruning goroutine runs forever; resolving config
	// once at startup is fine because the test setters don't change keepRecent.
	keepRecent, ok := s.fetchKeepRecent()
	if !ok || keepRecent <= 0 || pruneIntervalSeconds <= 0 {
		return
	}

	s.pruneWg.Add(1)
	go func() {
		defer s.pruneWg.Done()
		for {
			latestVersion := s.latestVersion.Load()
			pruneBeforeBlock := latestVersion - keepRecent
			if pruneBeforeBlock > 0 {
				pruned := s.PruneOldFiles(uint64(pruneBeforeBlock))
				if pruned > 0 {
					logger.Info("Pruned parquet file pairs older than block", "pruned-count", pruned, "block", pruneBeforeBlock)
				}
			}
			jitter := time.Duration(rand.Float64()*float64(pruneIntervalSeconds)*0.5) * time.Second
			sleepDuration := time.Duration(pruneIntervalSeconds)*time.Second + jitter

			select {
			case <-s.pruneStop:
				return
			case <-time.After(sleepDuration):
			}
		}
	}()
}

// fetchKeepRecent reads KeepRecent from coordinator state. Defined as a
// separate method so the pruning loop is readable.
func (s *Store) fetchKeepRecent() (int64, bool) {
	resp := make(chan int64, 1)
	if !s.send(coordinatorRequest{getKeepRecent: &getKeepRecentRequest{resp: resp}}) {
		return 0, false
	}
	return <-resp, true
}

// ----------------------------------------------------------------------------
// Snapshots and small utilities
// ----------------------------------------------------------------------------

func snapshot(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	return out
}

func addToClosedSorted(slice *[]string, path string) {
	for _, f := range *slice {
		if f == path {
			return
		}
	}
	*slice = append(*slice, path)
	sort.Slice(*slice, func(i, j int) bool {
		return ExtractBlockNumber((*slice)[i]) < ExtractBlockNumber((*slice)[j])
	})
}

func removeFromClosed(slice *[]string, startBlock uint64) {
	out := (*slice)[:0]
	for _, f := range *slice {
		if ExtractBlockNumber(f) != startBlock {
			out = append(out, f)
		}
	}
	*slice = out
}

func isRotationBoundary(blockNumber, maxBlocksPerFile uint64) bool {
	if maxBlocksPerFile == 0 {
		return false
	}
	return blockNumber%maxBlocksPerFile == 0
}

func alignedFileStartBlock(blockNumber, maxBlocksPerFile uint64) uint64 {
	if maxBlocksPerFile == 0 {
		return blockNumber
	}
	return (blockNumber / maxBlocksPerFile) * maxBlocksPerFile
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
