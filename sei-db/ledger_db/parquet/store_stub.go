//go:build !duckdb
// +build !duckdb

package parquet

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbwal "github.com/sei-protocol/sei-chain/sei-db/wal"
)

var errParquetDisabled = errors.New("parquet receipt store requires duckdb build tag")

// StoreConfig configures the parquet store.
type StoreConfig struct {
	DBDirectory          string
	KeepRecent           int64
	PruneIntervalSeconds int64
	BlockFlushInterval   uint64
	MaxBlocksPerFile     uint64
}

// DefaultStoreConfig returns the default store configuration.
func DefaultStoreConfig() StoreConfig {
	return StoreConfig{
		BlockFlushInterval: 1,
		MaxBlocksPerFile:   500,
	}
}

// ReceiptInput is the input for storing a receipt.
type ReceiptInput struct {
	BlockNumber  uint64
	Receipt      ReceiptRecord
	Logs         []LogRecord
	ReceiptBytes []byte
}

// Store is a stub for non-duckdb builds.
type Store struct {
	Reader        *Reader
	WarmupRecords []ReceiptRecord
}

// NewStore returns an error when duckdb is not enabled.
func NewStore(_ dbLogger.Logger, _ StoreConfig) (*Store, error) {
	return nil, errParquetDisabled
}

// LatestVersion returns 0 for the stub.
func (s *Store) LatestVersion() int64 { return 0 }

// SetLatestVersion is a no-op for the stub.
func (s *Store) SetLatestVersion(_ int64) {}

// SetEarliestVersion is a no-op for the stub.
func (s *Store) SetEarliestVersion(_ int64) {}

// CacheRotateInterval returns 0 for the stub.
func (s *Store) CacheRotateInterval() uint64 { return 0 }

// GetReceiptByTxHash returns an error for the stub.
func (s *Store) GetReceiptByTxHash(_ context.Context, _ common.Hash) (*ReceiptResult, error) {
	return nil, errParquetDisabled
}

// GetLogs returns an error for the stub.
func (s *Store) GetLogs(_ context.Context, _ LogFilter) ([]LogResult, error) {
	return nil, errParquetDisabled
}

// WriteReceipt returns an error for the stub.
func (s *Store) WriteReceipt(_ ReceiptInput) error {
	return errParquetDisabled
}

// WriteReceipts returns an error for the stub.
func (s *Store) WriteReceipts(_ []ReceiptInput) error {
	return errParquetDisabled
}

// UpdateLatestVersion is a no-op for the stub.
func (s *Store) UpdateLatestVersion(_ int64) {}

// Close is a no-op for the stub.
func (s *Store) Close() error { return nil }

// WAL returns nil for the stub.
func (s *Store) WAL() dbwal.GenericWAL[WALEntry] { return nil }

// ApplyReceiptFromReplay returns an error for the stub.
func (s *Store) ApplyReceiptFromReplay(_ ReceiptInput) error {
	return errParquetDisabled
}

// FileStartBlock returns 0 for the stub.
func (s *Store) FileStartBlock() uint64 { return 0 }

// ClearWAL is a no-op for the stub.
func (s *Store) ClearWAL() {}

// Uint32FromUint safely converts uint to uint32.
func Uint32FromUint(value uint) uint32 {
	const maxUint32 = ^uint32(0)
	if value > uint(maxUint32) {
		return maxUint32
	}
	return uint32(value)
}

// CopyBytes creates a copy of a byte slice.
func CopyBytes(src []byte) []byte {
	if len(src) == 0 {
		return make([]byte, 0)
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// CopyBytesOrEmpty creates a copy of a byte slice, returning empty slice for nil.
func CopyBytesOrEmpty(src []byte) []byte {
	if src == nil {
		return make([]byte, 0)
	}
	return CopyBytes(src)
}
