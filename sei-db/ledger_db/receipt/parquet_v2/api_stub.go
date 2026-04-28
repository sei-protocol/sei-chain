package parquet_v2

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

func (s *Store) WriteReceipts(inputs []parquet.ReceiptInput) error {
	_ = inputs
	return ErrNotImplemented
}

func (s *Store) GetReceiptByTxHash(ctx context.Context, txHash common.Hash) (*parquet.ReceiptResult, error) {
	_ = ctx
	_ = txHash
	return nil, ErrNotImplemented
}

func (s *Store) GetReceiptByTxHashInBlock(ctx context.Context, txHash common.Hash, blockNumber uint64) (*parquet.ReceiptResult, error) {
	_ = ctx
	_ = txHash
	_ = blockNumber
	return nil, ErrNotImplemented
}

func (s *Store) GetLogs(ctx context.Context, filter parquet.LogFilter) ([]parquet.LogResult, error) {
	_ = ctx
	_ = filter
	return nil, ErrNotImplemented
}

func (s *Store) ObserveEmptyBlock(height uint64) error {
	_ = height
	return ErrNotImplemented
}

func (s *Store) IsRotationBoundary(blockNumber uint64) bool {
	_ = blockNumber
	return false
}

func (s *Store) FileStartBlock() uint64 {
	return 0
}

func (s *Store) LatestVersion() int64 {
	return 0
}

func (s *Store) SetLatestVersion(version int64) {
	_ = version
}

func (s *Store) SetEarliestVersion(version int64) {
	_ = version
}

func (s *Store) UpdateLatestVersion(version int64) {
	_ = version
}

func (s *Store) CacheRotateInterval() uint64 {
	return 0
}

func (s *Store) Flush() error {
	return ErrNotImplemented
}

func (s *Store) Close() error {
	return ErrNotImplemented
}

func (s *Store) SimulateCrash() {
}

func (s *Store) SetBlockFlushInterval(interval uint64) {
	_ = interval
}

func (s *Store) SetMaxBlocksPerFile(n uint64) {
	_ = n
}

func (s *Store) SetFaultHooks(hooks *parquet.FaultHooks) {
	_ = hooks
}

func (s *Store) ReplayWAL(converter WALReceiptConverter) (ReplayResult, error) {
	_ = converter
	return ReplayResult{}, ErrNotImplemented
}
