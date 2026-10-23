package types

import (
	"fmt"
	"math"
)

// Record is an encoded primary key, value, and secondary lookup aliases.
type Record struct {
	Key     Key
	Value   []byte
	Aliases []Alias
}

// Alias identifies a byte range of a record's value under another key.
type Alias struct {
	Key Key
	ValueRange
}

// Encode returns an AppQC record ready for insertion.
func (record AppQCRecord) Encode() (Record, error) {
	return encodeAppRecord(kindAppQC, record.Version, record.First, record.Next, record.Data)
}

// Encode returns an AppProposal record ready for insertion.
func (record AppProposalRecord) Encode() (Record, error) {
	return encodeAppRecord(kindAppProp, record.Version, record.First, record.Next, record.Data)
}

// encodeAppRecord encodes a versioned application record and its covered-number aliases.
func encodeAppRecord(prefix byte, version RecordVersion, first, next BlockNumber, data []byte) (Record, error) {
	size := uint64(1) + uint64(len(data))
	if size >= math.MaxUint32 {
		return Record{}, fmt.Errorf("app record exceeds value size limit: %d bytes", size)
	}
	record := Record{Key: numberKey(prefix, first), Value: make([]byte, 1, int(size))} //nolint:gosec // size fits uint32
	record.Value[0] = byte(version)
	record.Value = append(record.Value, data...)
	location := ValueRange{Length: uint32(len(record.Value))} //nolint:gosec // size fits uint32
	for n := first + 1; n < next; n++ {
		record.Aliases = append(record.Aliases, Alias{Key: numberKey(prefix, n), ValueRange: location})
	}
	return record, nil
}
