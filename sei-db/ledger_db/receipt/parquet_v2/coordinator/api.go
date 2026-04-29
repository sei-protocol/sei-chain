package coordinator

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

func (c *Coordinator) WriteReceipts(inputs []parquet.ReceiptInput) error {
	resp := make(chan writeResp, 1)
	r, err := awaitResponse(c, writeReq{inputs: inputs, resp: resp}, resp)
	if err != nil {
		return err
	}
	return r.err
}

func (c *Coordinator) GetReceiptByTxHash(ctx context.Context, txHash common.Hash) (*parquet.ReceiptResult, error) {
	resp := make(chan readReceiptResp, 1)
	r, err := awaitResponse(c, readByTxHashReq{ctx: ctx, txHash: txHash, resp: resp}, resp)
	if err != nil {
		return nil, err
	}
	return r.result, r.err
}

func (c *Coordinator) GetReceiptByTxHashInBlock(ctx context.Context, txHash common.Hash, blockNumber uint64) (*parquet.ReceiptResult, error) {
	resp := make(chan readReceiptResp, 1)
	r, err := awaitResponse(c, readByTxHashInBlockReq{
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
	r, err := awaitResponse(c, getLogsReq{ctx: ctx, filter: filter, resp: resp}, resp)
	if err != nil {
		return nil, err
	}
	return r.results, r.err
}

func (c *Coordinator) ObserveEmptyBlock(height uint64) error {
	resp := make(chan error, 1)
	return awaitError(c, observeEmptyBlockReq{height: height, resp: resp}, resp)
}

func (c *Coordinator) IsRotationBoundary(blockNumber uint64) bool {
	resp := make(chan bool, 1)
	r, err := awaitResponse(c, isRotationBoundaryReq{blockNumber: blockNumber, resp: resp}, resp)
	if err != nil {
		return false
	}
	return r
}

func (c *Coordinator) FileStartBlock() uint64 {
	resp := make(chan uint64, 1)
	r, err := awaitResponse(c, fileStartBlockReq{resp: resp}, resp)
	if err != nil {
		return 0
	}
	return r
}

func (c *Coordinator) LatestVersion() int64 {
	resp := make(chan int64, 1)
	r, err := awaitResponse(c, latestVersionReq{resp: resp}, resp)
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

func (c *Coordinator) CacheRotateInterval() uint64 {
	resp := make(chan uint64, 1)
	r, err := awaitResponse(c, cacheRotateIntervalReq{resp: resp}, resp)
	if err != nil {
		return 0
	}
	return r
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
		_, _ = awaitResponse(c, simulateCrashReq{resp: resp}, resp)
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
	r, err := awaitResponse(c, replayWALReq{converter: converter, resp: resp}, resp)
	if err != nil {
		return ReplayResult{}, err
	}
	return r.result, r.err
}
