package coordinator

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

// coordRequest is the sealed-union marker for messages sent to the
// coordinator goroutine. Each concrete request type lives below and carries
// its own per-request reply channel.
type coordRequest interface {
	isCoordRequest()
}

// writeReq asks the coordinator to append receipts for a block. height is
// authoritative; per-input BlockNumber is ignored.
type writeReq struct {
	height uint64
	inputs []parquet.ReceiptInput
	resp   chan writeResp
}

// writeResp carries the outcome of a writeReq.
type writeResp struct {
	err error
}

// readByTxHashReq asks the coordinator to look up the earliest receipt for
// txHash. The temp write cache is consulted first, then closed parquet
// files.
type readByTxHashReq struct {
	ctx    context.Context
	txHash common.Hash
	resp   chan readReceiptResp
}

// readByTxHashInBlockReq asks for the receipt at exactly blockNumber, used
// to disambiguate replayed transactions across reorgs.
type readByTxHashInBlockReq struct {
	ctx         context.Context
	txHash      common.Hash
	blockNumber uint64
	resp        chan readReceiptResp
}

// readReceiptResp carries the outcome of a receipt read. result==nil with
// err==nil indicates "not found".
type readReceiptResp struct {
	result *parquet.ReceiptResult
	err    error
}

// getLogsReq asks the coordinator for logs matching filter across the
// closed log parquet files.
type getLogsReq struct {
	ctx    context.Context
	filter parquet.LogFilter
	resp   chan getLogsResp
}

// getLogsResp carries the outcome of a getLogsReq.
type getLogsResp struct {
	results []parquet.LogResult
	err     error
}

// flushReq asks the coordinator to flush buffered receipts/logs to the open
// parquet file.
type flushReq struct {
	resp chan error
}

// latestVersionReq asks for the highest block height observed by the
// coordinator.
type latestVersionReq struct {
	resp chan int64
}

// setLatestVersionReq overwrites latestVersion. Used when a caller knows
// the chain height authoritatively (e.g., genesis init).
type setLatestVersionReq struct {
	version int64
	resp    chan error
}

// setEarliestVersionReq records the lowest retained block height for
// pruning bookkeeping.
type setEarliestVersionReq struct {
	version int64
	resp    chan error
}

// updateLatestVersionReq advances latestVersion only when version is
// strictly greater, preventing rewinds.
type updateLatestVersionReq struct {
	version int64
	resp    chan error
}

// fileStartBlockReq asks for the start block of the currently open parquet
// file.
type fileStartBlockReq struct {
	resp chan uint64
}

// setBlockFlushIntervalReq updates how often (in blocks) the open writers
// are flushed to disk.
type setBlockFlushIntervalReq struct {
	interval uint64
	resp     chan error
}

// setMaxBlocksPerFileReq updates the rotation interval and propagates it
// to the reader.
type setMaxBlocksPerFileReq struct {
	maxBlocksPerFile uint64
	resp             chan error
}

// setFaultHooksReq installs test hooks. nil disables all hook checks.
type setFaultHooksReq struct {
	hooks *parquet.FaultHooks
	resp  chan error
}

// replayWALReq drives WAL replay using converter to decode receipt bytes
// into per-block records.
type replayWALReq struct {
	converter WALReceiptConverter
	resp      chan replayWALResp
}

// replayWALResp carries the recovered records and per-block tx hashes
// produced by replayWAL.
type replayWALResp struct {
	result ReplayResult
	err    error
}

// simulateCrashReq drops in-memory writer state without flushing so that
// recovery paths can be exercised. Test-only.
type simulateCrashReq struct {
	resp chan struct{}
}

// closeReq triggers a graceful shutdown: flush, close writers, close WAL
// and reader.
type closeReq struct {
	resp chan error
}

func (writeReq) isCoordRequest()                 {}
func (readByTxHashReq) isCoordRequest()          {}
func (readByTxHashInBlockReq) isCoordRequest()   {}
func (getLogsReq) isCoordRequest()               {}
func (flushReq) isCoordRequest()                 {}
func (latestVersionReq) isCoordRequest()         {}
func (setLatestVersionReq) isCoordRequest()      {}
func (setEarliestVersionReq) isCoordRequest()    {}
func (updateLatestVersionReq) isCoordRequest()   {}
func (fileStartBlockReq) isCoordRequest()        {}
func (setBlockFlushIntervalReq) isCoordRequest() {}
func (setMaxBlocksPerFileReq) isCoordRequest()   {}
func (setFaultHooksReq) isCoordRequest()         {}
func (replayWALReq) isCoordRequest()             {}
func (simulateCrashReq) isCoordRequest()         {}
func (closeReq) isCoordRequest()                 {}
