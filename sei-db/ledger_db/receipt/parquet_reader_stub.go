//go:build !duckdb
// +build !duckdb

package receipt

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"
)

var errDuckDBDisabled = errors.New("duckdb disabled; build with -tags duckdb")

type parquetReader struct{}

func newParquetReader(_ string) (*parquetReader, error) {
	return nil, errDuckDBDisabled
}

func (r *parquetReader) Close() error {
	return nil
}

func (r *parquetReader) onFileRotation(_ uint64) {}

func (r *parquetReader) closedReceiptFileCount() int { return 0 }

func (r *parquetReader) maxReceiptBlockNumber(_ context.Context) (uint64, bool, error) {
	return 0, false, errDuckDBDisabled
}

func (r *parquetReader) getReceiptByTxHash(_ context.Context, _ common.Hash) (*receiptResult, error) {
	return nil, errDuckDBDisabled
}

func (r *parquetReader) getLogs(_ context.Context, _ logFilter) ([]logResult, error) {
	return nil, errDuckDBDisabled
}
