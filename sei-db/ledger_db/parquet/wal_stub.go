//go:build !duckdb
// +build !duckdb

package parquet

import (
	"errors"

	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbwal "github.com/sei-protocol/sei-chain/sei-db/wal"
)

// WALEntry represents an entry in the parquet write-ahead log.
type WALEntry struct {
	ReceiptBytes []byte `json:"receipt_bytes"`
}

// NewWAL returns an error when duckdb is not enabled.
func NewWAL(_ dbLogger.Logger, _ string) (dbwal.GenericWAL[WALEntry], error) {
	return nil, errors.New("parquet WAL requires duckdb build tag")
}
