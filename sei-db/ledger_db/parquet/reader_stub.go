//go:build !duckdb
// +build !duckdb

package parquet

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

var errDuckDBDisabled = errors.New("duckdb disabled; build with -tags duckdb")

// Reader is a stub for non-duckdb builds.
type Reader struct{}

// FilePair represents a matched pair of receipt and log parquet files.
type FilePair struct {
	ReceiptFile string
	LogFile     string
	StartBlock  uint64
}

// NewReader returns an error when duckdb is not enabled.
func NewReader(_ string) (*Reader, error) {
	return nil, errDuckDBDisabled
}

// Close is a no-op for the stub.
func (r *Reader) Close() error {
	return nil
}

// OnFileRotation is a no-op for the stub.
func (r *Reader) OnFileRotation(_ uint64) {}

// ClosedReceiptFileCount returns 0 for the stub.
func (r *Reader) ClosedReceiptFileCount() int { return 0 }

// GetFilesBeforeBlock returns nil for the stub.
func (r *Reader) GetFilesBeforeBlock(_ uint64) []FilePair { return nil }

// RemoveFilesBeforeBlock is a no-op for the stub.
func (r *Reader) RemoveFilesBeforeBlock(_ uint64) {}

// MaxReceiptBlockNumber returns an error for the stub.
func (r *Reader) MaxReceiptBlockNumber(_ context.Context) (uint64, bool, error) {
	return 0, false, errDuckDBDisabled
}

// GetReceiptByTxHash returns an error for the stub.
func (r *Reader) GetReceiptByTxHash(_ context.Context, _ common.Hash) (*ReceiptResult, error) {
	return nil, errDuckDBDisabled
}

// GetLogs returns an error for the stub.
func (r *Reader) GetLogs(_ context.Context, _ LogFilter) ([]LogResult, error) {
	return nil, errDuckDBDisabled
}

// ExtractBlockNumber extracts the block number from a parquet filename.
func ExtractBlockNumber(path string) uint64 {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".parquet")
	parts := strings.Split(base, "_")
	if len(parts) < 2 {
		return 0
	}
	num, _ := strconv.ParseUint(parts[len(parts)-1], 10, 64)
	return num
}
