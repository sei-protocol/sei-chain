//go:build duckdb
// +build duckdb

package receipt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"

	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbwal "github.com/sei-protocol/sei-chain/sei-db/wal"
)

// parquetWALEntry stores all receipts for a single block.
// This batches writes per block instead of per receipt for better performance.
type parquetWALEntry struct {
	BlockNumber uint64
	Receipts    [][]byte // Each element is a marshaled receipt (protobuf)
}

func newParquetWAL(logger dbLogger.Logger, dir string) (dbwal.GenericWAL[parquetWALEntry], error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return dbwal.NewWAL(
		// Binary encoder - much faster than JSON
		func(entry parquetWALEntry) ([]byte, error) {
			return encodeWALEntry(entry)
		},
		func(data []byte) (parquetWALEntry, error) {
			return decodeWALEntry(data)
		},
		logger,
		dir,
		dbwal.Config{},
	)
}

// encodeWALEntry encodes a WAL entry to binary format:
// [blockNumber:8][numReceipts:4][len1:4][receipt1]...[lenN:4][receiptN]
func encodeWALEntry(entry parquetWALEntry) ([]byte, error) {
	// Calculate total size
	size := 8 + 4 // blockNumber + numReceipts
	for _, r := range entry.Receipts {
		size += 4 + len(r) // length prefix + data
	}

	buf := make([]byte, size)
	offset := 0

	// Write block number
	binary.LittleEndian.PutUint64(buf[offset:], entry.BlockNumber)
	offset += 8

	// Write number of receipts
	binary.LittleEndian.PutUint32(buf[offset:], uint32(len(entry.Receipts)))
	offset += 4

	// Write each receipt with length prefix
	for _, r := range entry.Receipts {
		binary.LittleEndian.PutUint32(buf[offset:], uint32(len(r)))
		offset += 4
		copy(buf[offset:], r)
		offset += len(r)
	}

	return buf, nil
}

// decodeWALEntry decodes a binary WAL entry.
func decodeWALEntry(data []byte) (parquetWALEntry, error) {
	var entry parquetWALEntry

	if len(data) < 12 {
		return entry, errors.New("WAL entry too short")
	}

	r := bytes.NewReader(data)

	// Read block number
	var blockNumber uint64
	if err := binary.Read(r, binary.LittleEndian, &blockNumber); err != nil {
		return entry, err
	}
	entry.BlockNumber = blockNumber

	// Read number of receipts
	var numReceipts uint32
	if err := binary.Read(r, binary.LittleEndian, &numReceipts); err != nil {
		return entry, err
	}

	entry.Receipts = make([][]byte, 0, numReceipts)

	// Read each receipt
	for i := uint32(0); i < numReceipts; i++ {
		var length uint32
		if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
			return entry, err
		}

		receipt := make([]byte, length)
		if _, err := r.Read(receipt); err != nil {
			return entry, err
		}
		entry.Receipts = append(entry.Receipts, receipt)
	}

	return entry, nil
}
