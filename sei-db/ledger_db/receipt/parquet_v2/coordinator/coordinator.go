package coordinator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	parquetgo "github.com/parquet-go/parquet-go"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	dbwal "github.com/sei-protocol/sei-chain/sei-db/wal"
)

type closedFile struct {
	startBlock  uint64
	receiptPath string
	logPath     string
}

// Coordinator owns parquet_v2's mutable state and serializes all access via
// its requests channel. Construct with New; interact through the typed
// methods (WriteReceipts, GetLogs, ...).
type Coordinator struct {
	requests    chan coordRequest
	pruneTick   <-chan time.Time
	pruneTicker *time.Ticker
	done        chan struct{}
	closeOnce   sync.Once

	config parquet.StoreConfig

	basePath       string
	fileStartBlock uint64
	receiptWriter  *parquetgo.GenericWriter[parquet.ReceiptRecord]
	logWriter      *parquetgo.GenericWriter[parquet.LogRecord]
	receiptFile    *os.File
	logFile        *os.File
	closedFiles    []closedFile

	receiptsBuffer   []parquet.ReceiptRecord
	logsBuffer       []parquet.LogRecord
	lastSeenBlock    uint64
	blocksSinceFlush uint64

	tempWriteCache map[common.Hash][]tempReceipt

	latestVersion   int64
	earliestVersion int64

	faultHooks *parquet.FaultHooks

	wal    dbwal.GenericWAL[parquet.WALEntry]
	reader *Reader
}

// New constructs a Coordinator with a live goroutine. The returned
// Coordinator is ready to accept requests via its typed methods.
func New(cfg parquet.StoreConfig) (*Coordinator, error) {
	storeCfg := resolveStoreConfig(cfg)

	if err := os.MkdirAll(storeCfg.DBDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create parquet base directory: %w", err)
	}

	requests := make(chan coordRequest)
	done := make(chan struct{})
	reader, err := NewReaderWithMaxBlocksPerFile(cfg.DBDirectory, storeCfg.MaxBlocksPerFile)
	if err != nil {
		return nil, err
	}
	cleanupReader := true
	defer func() {
		if cleanupReader {
			_ = reader.Close()
		}
	}()

	walDir := filepath.Join(storeCfg.DBDirectory, "parquet-wal")
	receiptWAL, err := parquet.NewWAL(walDir)
	if err != nil {
		return nil, err
	}
	cleanupWAL := true
	defer func() {
		if cleanupWAL {
			_ = receiptWAL.Close()
		}
	}()

	closedFiles, err := scanClosedFiles(storeCfg.DBDirectory, reader)
	if err != nil {
		return nil, err
	}

	c := &Coordinator{
		requests:        requests,
		done:            done,
		config:          storeCfg,
		basePath:        cfg.DBDirectory,
		closedFiles:     closedFiles,
		receiptsBuffer:  make([]parquet.ReceiptRecord, 0, 1000),
		logsBuffer:      make([]parquet.LogRecord, 0, 10000),
		tempWriteCache:  make(map[common.Hash][]tempReceipt),
		reader:          reader,
		wal:             receiptWAL,
		latestVersion:   0,
		earliestVersion: 0,
	}

	receiptFiles := make([]string, 0, len(closedFiles))
	for _, f := range closedFiles {
		receiptFiles = append(receiptFiles, f.receiptPath)
	}
	if maxBlock, ok, err := reader.MaxReceiptBlockNumber(context.Background(), receiptFiles); err != nil {
		return nil, err
	} else if ok {
		latest, err := int64FromUint64(maxBlock)
		if err != nil {
			return nil, err
		}
		c.latestVersion = latest
		if maxBlock < ^uint64(0) {
			c.fileStartBlock = maxBlock + 1
		}
	}

	if storeCfg.KeepRecent > 0 && storeCfg.PruneIntervalSeconds > 0 {
		c.pruneTicker = time.NewTicker(time.Duration(storeCfg.PruneIntervalSeconds) * time.Second)
		c.pruneTick = c.pruneTicker.C
	}

	go c.run()
	cleanupReader = false
	cleanupWAL = false

	return c, nil
}

func resolveStoreConfig(cfg parquet.StoreConfig) parquet.StoreConfig {
	resolved := parquet.DefaultStoreConfig()
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

func (c *Coordinator) run() {
	for {
		select {
		case req := <-c.requests:
			switch r := req.(type) {
			case writeReq:
				c.handleWrite(r)
			case readByTxHashReq:
				c.handleReadByTxHash(r)
			case readByTxHashInBlockReq:
				c.handleReadByTxHashInBlock(r)
			case getLogsReq:
				c.handleGetLogs(r)
			case flushReq:
				c.handleFlush(r)
			case latestVersionReq:
				c.handleLatestVersion(r)
			case setLatestVersionReq:
				c.handleSetLatestVersion(r)
			case setEarliestVersionReq:
				c.handleSetEarliestVersion(r)
			case updateLatestVersionReq:
				c.handleUpdateLatestVersion(r)
			case fileStartBlockReq:
				c.handleFileStartBlock(r)
			case setBlockFlushIntervalReq:
				c.handleSetBlockFlushInterval(r)
			case setMaxBlocksPerFileReq:
				c.handleSetMaxBlocksPerFile(r)
			case setFaultHooksReq:
				c.handleSetFaultHooks(r)
			case replayWALReq:
				c.handleReplayWAL(r)
			case simulateCrashReq:
				c.handleSimulateCrash(r)
				return
			case closeReq:
				c.handleClose(r)
				return
			}
		case <-c.pruneTick:
			c.handlePruneTick()
		case <-c.done:
			c.stopPruneTicker()
			return
		}
	}
}

func (c *Coordinator) stopPruneTicker() {
	if c.pruneTicker == nil {
		return
	}
	c.pruneTicker.Stop()
	c.pruneTicker = nil
	c.pruneTick = nil
}

// WriteReceipts records a committed block. inputs may be empty, in which case
// the call only advances rotation/cursor state — equivalent to the former
// ObserveEmptyBlock. height is authoritative; inputs[i].BlockNumber is
// ignored.
func (c *Coordinator) WriteReceipts(height uint64, inputs []parquet.ReceiptInput) error {
	resp := make(chan writeResp, 1)
	r, err := sendAndAwaitResponse(c, writeReq{height: height, inputs: inputs, resp: resp}, resp)
	if err != nil {
		return err
	}
	return r.err
}

func (c *Coordinator) GetReceiptByTxHash(ctx context.Context, txHash common.Hash) (*parquet.ReceiptResult, error) {
	resp := make(chan readReceiptResp, 1)
	r, err := sendAndAwaitResponse(c, readByTxHashReq{ctx: ctx, txHash: txHash, resp: resp}, resp)
	if err != nil {
		return nil, err
	}
	return r.result, r.err
}

func (c *Coordinator) GetReceiptByTxHashInBlock(ctx context.Context, txHash common.Hash, blockNumber uint64) (*parquet.ReceiptResult, error) {
	resp := make(chan readReceiptResp, 1)
	r, err := sendAndAwaitResponse(c, readByTxHashInBlockReq{
		ctx:         ctx,
		txHash:      txHash,
		blockNumber: blockNumber,
		resp:        resp,
	}, resp)
	if err != nil {
		return nil, err
	}
	return r.result, r.err
}

func (c *Coordinator) GetLogs(ctx context.Context, filter parquet.LogFilter) ([]parquet.LogResult, error) {
	resp := make(chan getLogsResp, 1)
	r, err := sendAndAwaitResponse(c, getLogsReq{ctx: ctx, filter: filter, resp: resp}, resp)
	if err != nil {
		return nil, err
	}
	return r.results, r.err
}

func (c *Coordinator) FileStartBlock() uint64 {
	resp := make(chan uint64, 1)
	r, err := sendAndAwaitResponse(c, fileStartBlockReq{resp: resp}, resp)
	if err != nil {
		return 0
	}
	return r
}

func (c *Coordinator) LatestVersion() int64 {
	resp := make(chan int64, 1)
	r, err := sendAndAwaitResponse(c, latestVersionReq{resp: resp}, resp)
	if err != nil {
		return 0
	}
	return r
}

func (c *Coordinator) SetLatestVersion(version int64) {
	resp := make(chan error, 1)
	_ = awaitError(c, setLatestVersionReq{version: version, resp: resp}, resp)
}

func (c *Coordinator) SetEarliestVersion(version int64) {
	resp := make(chan error, 1)
	_ = awaitError(c, setEarliestVersionReq{version: version, resp: resp}, resp)
}

func (c *Coordinator) UpdateLatestVersion(version int64) {
	resp := make(chan error, 1)
	_ = awaitError(c, updateLatestVersionReq{version: version, resp: resp}, resp)
}

// CacheRotateInterval returns the rotation boundary used by the cached receipt
// store. Reads c.config.MaxBlocksPerFile directly without going through the
// request channel; this is safe because the value is set at construction and
// only mutated by SetMaxBlocksPerFile, which is test-only and must not race
// with reads.
func (c *Coordinator) CacheRotateInterval() uint64 {
	return c.config.MaxBlocksPerFile
}

func (c *Coordinator) Flush() error {
	resp := make(chan error, 1)
	return awaitError(c, flushReq{resp: resp}, resp)
}

func (c *Coordinator) Close() error {
	var err error
	c.closeOnce.Do(func() {
		resp := make(chan error, 1)
		err = awaitError(c, closeReq{resp: resp}, resp)
		close(c.done)
	})
	return err
}

func (c *Coordinator) SimulateCrash() {
	c.closeOnce.Do(func() {
		resp := make(chan struct{}, 1)
		_, _ = sendAndAwaitResponse(c, simulateCrashReq{resp: resp}, resp)
		close(c.done)
	})
}

func (c *Coordinator) SetBlockFlushInterval(interval uint64) {
	resp := make(chan error, 1)
	_ = awaitError(c, setBlockFlushIntervalReq{interval: interval, resp: resp}, resp)
}

func (c *Coordinator) SetMaxBlocksPerFile(n uint64) {
	resp := make(chan error, 1)
	_ = awaitError(c, setMaxBlocksPerFileReq{maxBlocksPerFile: n, resp: resp}, resp)
}

func (c *Coordinator) SetFaultHooks(hooks *parquet.FaultHooks) {
	resp := make(chan error, 1)
	_ = awaitError(c, setFaultHooksReq{hooks: hooks, resp: resp}, resp)
}

func (c *Coordinator) ReplayWAL(converter WALReceiptConverter) (ReplayResult, error) {
	resp := make(chan replayWALResp, 1)
	r, err := sendAndAwaitResponse(c, replayWALReq{converter: converter, resp: resp}, resp)
	if err != nil {
		return ReplayResult{}, err
	}
	return r.result, r.err
}

func sendAndAwaitResponse[T any](c *Coordinator, req coordRequest, resp <-chan T) (T, error) {
	var zero T

	select {
	case c.requests <- req:
	case <-c.done:
		return zero, ErrStoreClosed
	}

	select {
	case r := <-resp:
		return r, nil
	case <-c.done:
		return zero, ErrStoreClosed
	}
}

func awaitError(c *Coordinator, req coordRequest, resp <-chan error) error {
	err, waitErr := sendAndAwaitResponse(c, req, resp)
	if waitErr != nil {
		return waitErr
	}
	return err
}
