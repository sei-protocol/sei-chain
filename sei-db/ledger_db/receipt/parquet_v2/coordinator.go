package parquet_v2

import (
	"os"
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

type coordinator struct {
	requests    chan coordRequest
	pruneTick   <-chan time.Time
	pruneTicker *time.Ticker
	done        chan struct{}

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

func (c *coordinator) run() {
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

func (c *coordinator) stopPruneTicker() {
	if c.pruneTicker == nil {
		return
	}
	c.pruneTicker.Stop()
	c.pruneTicker = nil
	c.pruneTick = nil
}
