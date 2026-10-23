package types

import (
	"encoding/binary"
	"fmt"
	"math"
)

// ValueRange identifies a value's byte range within an encoded QC group.
type ValueRange struct {
	Offset uint32
	Length uint32
}

// Encode returns an atomic QC and block record ready for insertion.
func (qc QCRecord) Encode() (Record, error) {
	if len(qc.Blocks) == 0 {
		return Record{}, fmt.Errorf("QC group requires blocks")
	}
	size := uint64(1+4) + uint64(len(qc.Data)) + 4*uint64(len(qc.Blocks))
	for _, block := range qc.Blocks {
		if block.Number == math.MaxUint64 {
			return Record{}, fmt.Errorf("block number %d has no exclusive upper bound", block.Number)
		}
		size += uint64(len(block.Value))
	}
	if size >= math.MaxUint32 {
		return Record{}, fmt.Errorf("QC group exceeds value size limit: %d bytes", size)
	}
	record := Record{
		Key:     numberKey(kindQCGroup, qc.Blocks[0].Number),
		Value:   make([]byte, 0, int(size)), //nolint:gosec // size fits uint32
		Aliases: make([]Alias, 0, 3*len(qc.Blocks)),
	}
	record.Value = append(record.Value, byte(qc.Version))
	var qcRange ValueRange
	record.Value, qcRange = appendGroupValue(record.Value, qc.Data)
	for _, block := range qc.Blocks {
		var location ValueRange
		record.Value, location = appendGroupValue(record.Value, block.Value)
		record.Aliases = append(record.Aliases,
			Alias{Key: numberKey(kindQC, block.Number), ValueRange: qcRange},
			Alias{Key: numberKey(kindBlock, block.Number), ValueRange: location},
			Alias{Key: BlockHashKey(block.Hash), ValueRange: location})
	}
	return record, nil
}

// appendGroupValue appends a framed value and returns its byte range.
func appendGroupValue(group, value []byte) ([]byte, ValueRange) {
	length := uint32(len(value)) //nolint:gosec // QCRecord.Encode bounds the total size before encoding
	group = binary.BigEndian.AppendUint32(group, length)
	location := ValueRange{Offset: uint32(len(group)), Length: length} //nolint:gosec // offset fits within the validated group size
	return append(group, value...), location
}

// DecodeQCGroup reads a versioned group containing length-prefixed QC and block values.
func DecodeQCGroup(value []byte) ([]byte, [][]byte, error) {
	value, err := ConsumeVersion(value)
	if err != nil {
		return nil, nil, err
	}
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
