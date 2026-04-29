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
	nextWriteOrdinal uint64

	tempWriteCache map[common.Hash][]tempReceipt

	latestVersion   int64
	earliestVersion int64

	replayedWarmup []parquet.ReceiptRecord
	replayedBlocks []ReplayedBlock

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
			case observeEmptyBlockReq:
				c.handleObserveEmptyBlock(r)
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
			case cacheRotateIntervalReq:
				c.handleCacheRotateInterval(r)
			case fileStartBlockReq:
				c.handleFileStartBlock(r)
			case isRotationBoundaryReq:
				c.handleIsRotationBoundary(r)
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

func awaitResponse[T any](c *Coordinator, req coordRequest, resp <-chan T) (T, error) {
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
	err, waitErr := awaitResponse(c, req, resp)
	if waitErr != nil {
		return waitErr
	}
	return err
}
