package vtype

import (
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

| Version | Value        |
|---------|--------------|
| 1 byte  | variable     |

Data is stored in big-endian order. Value is variable length.
*/

const (
	legacyVersionStart = 0
	legacyValueStart   = legacyVersionStart + VersionLength
	legacyHeaderLength = VersionLength
)

var _ VType = (*LegacyData)(nil)

// Used for encapsulating and serializing legacy data in the FlatKV legacy database.
//
// This data structure is not threadsafe. Values passed into and values received from this data structure
// are not safe to modify without first copying them.
type LegacyData struct {
	version LegacyDataVersion
	value   []byte
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
		version: version,
		value:   value,
	}, nil
}

// Get the serialization version for this LegacyData instance.
func (l *LegacyData) GetSerializationVersion() LegacyDataVersion {
	if l == nil {
		return LegacyDataVersion0
	}
	return l.version
}

// Get the legacy value.
func (l *LegacyData) GetValue() []byte {
	if l == nil {
		return []byte{}
	}
	return l.value
}

// Set the legacy value. Returns self (or a new LegacyData if nil).
func (l *LegacyData) SetValue(value []byte) *LegacyData {
	if l == nil {
		l = NewLegacyData()
	}
	l.value = append([]byte(nil), value...)
	return l
}

// Check if this legacy data signifies a deletion operation. A deletion operation is automatically
// performed when the value is empty (with the exception of the serialization version).
func (l *LegacyData) IsDelete() bool {
	if l == nil {
		return true
	}
	return len(l.value) == 0
}
