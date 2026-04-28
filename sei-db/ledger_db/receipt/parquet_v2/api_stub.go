package parquet_v2

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

func (s *Store) WriteReceipts(inputs []parquet.ReceiptInput) error {
	resp := make(chan writeResp, 1)
	r, err := awaitResponse(s, writeReq{inputs: inputs, resp: resp}, resp)
	if err != nil {
		return err
	}
	return r.err
}

func (s *Store) GetReceiptByTxHash(ctx context.Context, txHash common.Hash) (*parquet.ReceiptResult, error) {
	resp := make(chan readReceiptResp, 1)
	r, err := awaitResponse(s, readByTxHashReq{ctx: ctx, txHash: txHash, resp: resp}, resp)
	if err != nil {
		return nil, err
	}
	return r.result, r.err
}

func (s *Store) GetReceiptByTxHashInBlock(ctx context.Context, txHash common.Hash, blockNumber uint64) (*parquet.ReceiptResult, error) {
	resp := make(chan readReceiptResp, 1)
	r, err := awaitResponse(s, readByTxHashInBlockReq{
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

func (s *Store) GetLogs(ctx context.Context, filter parquet.LogFilter) ([]parquet.LogResult, error) {
	resp := make(chan getLogsResp, 1)
	r, err := awaitResponse(s, getLogsReq{ctx: ctx, filter: filter, resp: resp}, resp)
	if err != nil {
		return nil, err
	}
	return r.results, r.err
}

func (s *Store) ObserveEmptyBlock(height uint64) error {
	resp := make(chan error, 1)
	return awaitError(s, observeEmptyBlockReq{height: height, resp: resp}, resp)
}

func (s *Store) IsRotationBoundary(blockNumber uint64) bool {
	resp := make(chan bool, 1)
	r, err := awaitResponse(s, isRotationBoundaryReq{blockNumber: blockNumber, resp: resp}, resp)
	if err != nil {
		return false
	}
	return r
}

func (s *Store) FileStartBlock() uint64 {
	resp := make(chan uint64, 1)
	r, err := awaitResponse(s, fileStartBlockReq{resp: resp}, resp)
	if err != nil {
		return 0
	}
	return r
}

func (s *Store) LatestVersion() int64 {
	resp := make(chan int64, 1)
	r, err := awaitResponse(s, latestVersionReq{resp: resp}, resp)
	if err != nil {
		return 0
	}
	return r
}

func (s *Store) SetLatestVersion(version int64) {
	resp := make(chan error, 1)
	_ = awaitError(s, setLatestVersionReq{version: version, resp: resp}, resp)
}

func (s *Store) SetEarliestVersion(version int64) {
	resp := make(chan error, 1)
	_ = awaitError(s, setEarliestVersionReq{version: version, resp: resp}, resp)
}

func (s *Store) UpdateLatestVersion(version int64) {
	resp := make(chan error, 1)
	_ = awaitError(s, updateLatestVersionReq{version: version, resp: resp}, resp)
}

func (s *Store) CacheRotateInterval() uint64 {
	resp := make(chan uint64, 1)
	r, err := awaitResponse(s, cacheRotateIntervalReq{resp: resp}, resp)
	if err != nil {
		return 0
	}
	return r
}

func (s *Store) Flush() error {
	resp := make(chan error, 1)
	return awaitError(s, flushReq{resp: resp}, resp)
}

func (s *Store) Close() error {
	var err error
	s.closeOnce.Do(func() {
		resp := make(chan error, 1)
		err = awaitError(s, closeReq{resp: resp}, resp)
		close(s.done)
	})
	return err
}

func (s *Store) SimulateCrash() {
	s.closeOnce.Do(func() {
		resp := make(chan struct{}, 1)
		_, _ = awaitResponse(s, simulateCrashReq{resp: resp}, resp)
		close(s.done)
	})
}

func (s *Store) SetBlockFlushInterval(interval uint64) {
	resp := make(chan error, 1)
	_ = awaitError(s, setBlockFlushIntervalReq{interval: interval, resp: resp}, resp)
}

func (s *Store) SetMaxBlocksPerFile(n uint64) {
	resp := make(chan error, 1)
	_ = awaitError(s, setMaxBlocksPerFileReq{maxBlocksPerFile: n, resp: resp}, resp)
}

func (s *Store) SetFaultHooks(hooks *parquet.FaultHooks) {
	resp := make(chan error, 1)
	_ = awaitError(s, setFaultHooksReq{hooks: hooks, resp: resp}, resp)
}

func (s *Store) ReplayWAL(converter WALReceiptConverter) (ReplayResult, error) {
	resp := make(chan replayWALResp, 1)
	r, err := awaitResponse(s, replayWALReq{converter: converter, resp: resp}, resp)
	if err != nil {
		return ReplayResult{}, err
	}
	return r.result, r.err
}
