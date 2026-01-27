package receipt

import (
	"encoding/json"
	"os"

	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbwal "github.com/sei-protocol/sei-chain/sei-db/wal"
)

type parquetWALEntry struct {
	ReceiptBytes []byte `json:"receipt_bytes"`
}

func newParquetWAL(logger dbLogger.Logger, dir string) (dbwal.GenericWAL[parquetWALEntry], error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return dbwal.NewWAL(
		func(entry parquetWALEntry) ([]byte, error) {
			return json.Marshal(entry)
		},
		func(data []byte) (parquetWALEntry, error) {
			var entry parquetWALEntry
			err := json.Unmarshal(data, &entry)
			return entry, err
		},
		logger,
		dir,
		dbwal.Config{},
	)
}
