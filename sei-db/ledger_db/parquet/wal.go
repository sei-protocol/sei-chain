package parquet

import (
	"encoding/binary"
	"fmt"
	"os"

	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbwal "github.com/sei-protocol/sei-chain/sei-db/wal"
)

// WALEntry represents a batch of receipts for a single block in the WAL.
type WALEntry struct {
	BlockNumber uint64
	Receipts    [][]byte
}

// encodeWALEntry encodes a WALEntry to binary format:
// [blockNumber:8][numReceipts:4][len1:4][receipt1]...[lenN:4][receiptN]
func encodeWALEntry(entry WALEntry) ([]byte, error) {
	if err := validateUint32Int(len(entry.Receipts), "receipt count"); err != nil {
		return nil, err
	}

	size := 8 + 4 // blockNumber + numReceipts
	for _, r := range entry.Receipts {
		if err := validateUint32Int(len(r), "receipt length"); err != nil {
			return nil, err
		}
		size += 4 + len(r) // length prefix + data
	}

	buf := make([]byte, size)
	offset := 0

	binary.LittleEndian.PutUint64(buf[offset:], entry.BlockNumber)
	offset += 8

	putUint32FromInt(buf[offset:], len(entry.Receipts))
	offset += 4

	for _, r := range entry.Receipts {
		if err := validateUint32Int(len(r), "receipt length"); err != nil {
			return nil, err
		}
		putUint32FromInt(buf[offset:], len(r))
		offset += 4
		copy(buf[offset:], r)
		offset += len(r)
	}

	return buf, nil
}

// decodeWALEntry decodes a binary WALEntry.
func decodeWALEntry(data []byte) (WALEntry, error) {
	if len(data) < 12 {
		return WALEntry{}, fmt.Errorf("WAL entry too short: %d bytes", len(data))
	}

	offset := 0
	blockNumber := binary.LittleEndian.Uint64(data[offset:])
	offset += 8

	numReceipts := binary.LittleEndian.Uint32(data[offset:])
	offset += 4

	receipts := make([][]byte, 0, numReceipts)
	for i := uint32(0); i < numReceipts; i++ {
		if offset+4 > len(data) {
			return WALEntry{}, fmt.Errorf("WAL entry truncated at receipt %d length", i)
		}
		rLen := binary.LittleEndian.Uint32(data[offset:])
		offset += 4

		if offset+int(rLen) > len(data) {
			return WALEntry{}, fmt.Errorf("WAL entry truncated at receipt %d data", i)
		}
		r := make([]byte, rLen)
		copy(r, data[offset:offset+int(rLen)])
		offset += int(rLen)

		receipts = append(receipts, r)
	}

	return WALEntry{
		BlockNumber: blockNumber,
		Receipts:    receipts,
	}, nil
}

func validateUint32Int(value int, field string) error {
	if value < 0 || uint64(value) > uint64(maxUint32) {
		return fmt.Errorf("%s exceeds uint32 range: %d", field, value)
	}
	return nil
}

func putUint32FromInt(dst []byte, value int) {
	dst[0] = byte(value)
	dst[1] = byte(value >> 8)
	dst[2] = byte(value >> 16)
	dst[3] = byte(value >> 24)
}

// NewWAL creates a new WAL for parquet receipts.
func NewWAL(logger dbLogger.Logger, dir string) (dbwal.GenericWAL[WALEntry], error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return dbwal.NewWAL(
		encodeWALEntry,
		decodeWALEntry,
		logger,
		dir,
		dbwal.Config{},
	)
}
