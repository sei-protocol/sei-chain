package parquet_v2

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

type coordRequest interface {
	isCoordRequest()
}

type writeReq struct {
	inputs []parquet.ReceiptInput
	resp   chan writeResp
}

type writeResp struct {
	err error
}

type readByTxHashReq struct {
	ctx    context.Context
	txHash common.Hash
	resp   chan readReceiptResp
}

type readByTxHashInBlockReq struct {
	ctx         context.Context
	txHash      common.Hash
	blockNumber uint64
	resp        chan readReceiptResp
}

type readReceiptResp struct {
	result *parquet.ReceiptResult
	err    error
}

type getLogsReq struct {
	ctx    context.Context
	filter parquet.LogFilter
	resp   chan getLogsResp
}

type getLogsResp struct {
	results []parquet.LogResult
	err     error
}

type observeEmptyBlockReq struct {
	height uint64
	resp   chan error
}

type flushReq struct {
	resp chan error
}

type latestVersionReq struct {
	resp chan int64
}

type setLatestVersionReq struct {
	version int64
	resp    chan error
}

type setEarliestVersionReq struct {
	version int64
	resp    chan error
}

type updateLatestVersionReq struct {
	version int64
	resp    chan error
}

type cacheRotateIntervalReq struct {
	resp chan uint64
}

type fileStartBlockReq struct {
	resp chan uint64
}

type isRotationBoundaryReq struct {
	blockNumber uint64
	resp        chan bool
}

type setBlockFlushIntervalReq struct {
	interval uint64
	resp     chan error
}

type setMaxBlocksPerFileReq struct {
	maxBlocksPerFile uint64
	resp             chan error
}

type setFaultHooksReq struct {
	hooks *parquet.FaultHooks
	resp  chan error
}

type replayWALReq struct {
	converter WALReceiptConverter
	resp      chan replayWALResp
}

type replayWALResp struct {
	result ReplayResult
	err    error
}

type simulateCrashReq struct {
	resp chan struct{}
}

type closeReq struct {
	resp chan error
}

func (writeReq) isCoordRequest()                 {}
func (readByTxHashReq) isCoordRequest()          {}
func (readByTxHashInBlockReq) isCoordRequest()   {}
func (getLogsReq) isCoordRequest()               {}
func (observeEmptyBlockReq) isCoordRequest()     {}
func (flushReq) isCoordRequest()                 {}
func (latestVersionReq) isCoordRequest()         {}
func (setLatestVersionReq) isCoordRequest()      {}
func (setEarliestVersionReq) isCoordRequest()    {}
func (updateLatestVersionReq) isCoordRequest()   {}
func (cacheRotateIntervalReq) isCoordRequest()   {}
func (fileStartBlockReq) isCoordRequest()        {}
func (isRotationBoundaryReq) isCoordRequest()    {}
func (setBlockFlushIntervalReq) isCoordRequest() {}
func (setMaxBlocksPerFileReq) isCoordRequest()   {}
func (setFaultHooksReq) isCoordRequest()         {}
func (replayWALReq) isCoordRequest()             {}
func (simulateCrashReq) isCoordRequest()         {}
func (closeReq) isCoordRequest()                 {}
