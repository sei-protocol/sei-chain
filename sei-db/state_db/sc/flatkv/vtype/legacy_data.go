package vtype

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type LegacyDataVersion uint8

// DO NOT CHANGE VERSION VALUES!!! Adding new versions is ok, but historical versions should never be removed/changed.
const (
	LegacyDataVersion0 LegacyDataVersion = 0
)

/*
Serialization schema for LegacyData version 0:

| Version | Block Height | Value    |
|---------|--------------|----------|
| 1 byte  | 8 bytes      | variable |

Data is stored in big-endian order. Value is variable length.
*/

const (
	legacyVersionStart     = 0
	legacyBlockHeightStart = legacyVersionStart + VersionLength
	legacyValueStart       = legacyBlockHeightStart + BlockHeightLength
	legacyHeaderLength     = VersionLength + BlockHeightLength
)

var _ VType = (*LegacyData)(nil)

// Used for encapsulating and serializing legacy data in the FlatKV legacy database.
//
// This data structure is not threadsafe. Values passed into and values received from this data structure
// are not safe to modify without first copying them.
type LegacyData struct {
	version     LegacyDataVersion
	blockHeight int64
	value       []byte
	isDelete    bool
}

// Create a new LegacyData with the given value.
func NewLegacyData() *LegacyData {
	return &LegacyData{version: LegacyDataVersion0}
}

// Serialize the legacy data to a byte slice.
func (l *LegacyData) Serialize() []byte {
	if l == nil {
		return make([]byte, legacyHeaderLength)
	}
	data := make([]byte, legacyHeaderLength+len(l.value))
	data[legacyVersionStart] = byte(l.version)
	binary.BigEndian.PutUint64(data[legacyBlockHeightStart:legacyValueStart], uint64(l.blockHeight)) //nolint:gosec
	copy(data[legacyValueStart:], l.value)
	return data
}

// Deserialize the legacy data from the given byte slice.
func DeserializeLegacyData(data []byte) (*LegacyData, error) {
	if len(data) == 0 {
		return nil, errors.New("data is empty")
	}

	version := LegacyDataVersion(data[legacyVersionStart])
	if version != LegacyDataVersion0 {
		return nil, fmt.Errorf("unsupported serialization version: %d", version)
	}

	if len(data) < legacyHeaderLength {
		return nil, fmt.Errorf("data length at version %d should be at least %d, got %d",
			version, legacyHeaderLength, len(data))
	}

	value := make([]byte, len(data)-legacyHeaderLength)
	copy(value, data[legacyValueStart:])

	return &LegacyData{
		version:     version,
		blockHeight: int64(binary.BigEndian.Uint64(data[legacyBlockHeightStart:legacyValueStart])), //nolint:gosec
		value:       value,
	}, nil
}

// Get the serialization version for this LegacyData instance.
func (l *LegacyData) GetSerializationVersion() LegacyDataVersion {
	if l == nil {
		return LegacyDataVersion0
	}
	return l.version
}

// Get the block height when this legacy entry was last modified.
func (l *LegacyData) GetBlockHeight() int64 {
	if l == nil {
		return 0
	}
	return l.blockHeight
}

// Get the legacy value.
func (l *LegacyData) GetValue() []byte {
	if l == nil {
		return []byte{}
	}
	return l.value
}

// Set the block height when this legacy entry was last modified/touched. Returns self (or a new LegacyData if nil).
func (l *LegacyData) SetBlockHeight(blockHeight int64) *LegacyData {
	if l == nil {
		l = NewLegacyData()
	}
	l.blockHeight = blockHeight
	return l
}

// Set the legacy value. Returns self (or a new LegacyData if nil).
// Clears the delete flag — an explicit SetValue is a write, not a deletion,
// even when value is empty ([]byte{} is a valid Cosmos module value).
func (l *LegacyData) SetValue(value []byte) *LegacyData {
	if l == nil {
		l = NewLegacyData()
	}
	l.value = append([]byte(nil), value...)
	l.isDelete = false
	return l
}

// MarkDeleted flags this entry for physical key removal at commit time.
// The stored value is irrelevant once marked; IsDelete() will return true.
func (l *LegacyData) MarkDeleted() *LegacyData {
	if l == nil {
		l = NewLegacyData()
	}
	l.isDelete = true
	return l
}

// IsDelete reports whether this entry represents a deletion.
// Uses an explicit flag rather than value-length inference so that empty
// values ([]byte{}) written by Cosmos modules are not misinterpreted as
// deletions.
func (l *LegacyData) IsDelete() bool {
	if l == nil {
		return true
	}
	return l.isDelete
}
