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

	tempWriteCache map[common.Hash]tempReceipt

	latestVersion   int64
	earliestVersion int64

	replayedWarmup []parquet.ReceiptRecord
	replayedBlocks []ReplayedBlock

	faultHooks *parquet.FaultHooks

	wal    dbwal.GenericWAL[parquet.WALEntry]
	reader *Reader
}

func (c *coordinator) run() {
	_ = c
}
