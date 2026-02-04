//go:build duckdb
// +build duckdb

package parquet

import (
	"encoding/json"
	"os"

	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbwal "github.com/sei-protocol/sei-chain/sei-db/wal"
)

// WALEntry represents an entry in the parquet write-ahead log.
type WALEntry struct {
	ReceiptBytes []byte `json:"receipt_bytes"`
}

// NewWAL creates a new WAL for parquet receipts.
func NewWAL(logger dbLogger.Logger, dir string) (dbwal.GenericWAL[WALEntry], error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return dbwal.NewWAL(
		func(entry WALEntry) ([]byte, error) {
			return json.Marshal(entry)
		},
		func(data []byte) (WALEntry, error) {
			var entry WALEntry
			err := json.Unmarshal(data, &entry)
			return entry, err
		},
		logger,
		dir,
		dbwal.Config{},
	)
}
