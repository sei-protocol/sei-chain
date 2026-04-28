package parquet_v2

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

// Reader is the V2 DuckDB query helper. It intentionally owns no file-list
// state; callers pass explicit file snapshots to each query.
type Reader struct{}

func NewReader(basePath string) (*Reader, error) {
	_ = basePath
	return &Reader{}, nil
}

func NewReaderWithMaxBlocksPerFile(basePath string, maxBlocksPerFile uint64) (*Reader, error) {
	_ = basePath
	_ = maxBlocksPerFile
	return &Reader{}, nil
}

func (r *Reader) Close() error {
	_ = r
	return ErrNotImplemented
}

func (r *Reader) QueryReceiptByTxHash(ctx context.Context, files []string, txHash common.Hash) (*parquet.ReceiptResult, error) {
	_ = r
	_ = ctx
	_ = files
	_ = txHash
	return nil, ErrNotImplemented
}

func (r *Reader) QueryReceiptByTxHashInBlock(ctx context.Context, files []string, txHash common.Hash, blockNumber uint64) (*parquet.ReceiptResult, error) {
	_ = r
	_ = ctx
	_ = files
	_ = txHash
	_ = blockNumber
	return nil, ErrNotImplemented
}

func (r *Reader) QueryLogs(ctx context.Context, files []string, filter parquet.LogFilter) ([]parquet.LogResult, error) {
	_ = r
	_ = ctx
	_ = files
	_ = filter
	return nil, ErrNotImplemented
}

func (r *Reader) MaxReceiptBlockNumber(ctx context.Context, files []string) (uint64, bool, error) {
	_ = r
	_ = ctx
	_ = files
	return 0, false, ErrNotImplemented
}
