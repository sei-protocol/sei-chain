package types

import (
	"encoding/binary"
	"fmt"
	"math"
)

// EncodeQCGroup frames a QC and its blocks as a sequence of length-prefixed values.
func EncodeQCGroup(qc []byte, blocks []BlockRecord) ([]byte, error) {
	size := uint64(4) + uint64(len(qc)) + 4*uint64(len(blocks))
	for _, block := range blocks {
		size += uint64(len(block.Value))
	}
	if size >= math.MaxUint32 {
		return nil, fmt.Errorf("QC group exceeds value size limit: %d bytes", size)
	}
	value := make([]byte, 0, int(size))                           //nolint:gosec // size fits uint32
	value = binary.BigEndian.AppendUint32(value, uint32(len(qc))) //nolint:gosec // length fits within size
	value = append(value, qc...)
	for _, block := range blocks {
		value = binary.BigEndian.AppendUint32(value, uint32(len(block.Value))) //nolint:gosec // length fits within size
		value = append(value, block.Value...)
	}
	return value, nil
}

// DecodeQCGroup returns the QC and block values from a length-prefixed sequence.
func DecodeQCGroup(value []byte) ([]byte, [][]byte, error) {
	var records [][]byte
	for len(value) > 0 {
		if len(value) < 4 {
			return nil, nil, fmt.Errorf("truncated QC group length")
		}
		length := uint64(binary.BigEndian.Uint32(value))
		value = value[4:]
		if length > uint64(len(value)) {
			return nil, nil, fmt.Errorf("truncated QC group value")
		}
		records = append(records, value[:length])
		value = value[length:]
	}
	if len(records) < 2 {
		return nil, nil, fmt.Errorf("QC group must contain a QC and blocks")
	}
	return records[0], records[1:], nil
}
