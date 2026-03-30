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

| Version | Block Height | Value        |
|---------|--------------|--------------|
| 1 byte  | 8 bytes      | variable     |

Data is stored in big-endian order. Value is variable length.
*/

const (
	legacyVersionStart     = 0
	legacyBlockHeightStart = 1
	legacyValueStart       = 9
	legacyHeaderLength     = 9
)

// Used for encapsulating and serializing legacy data in the FlatKV legacy database.
//
// This data structure is not threadsafe. Values passed into and values received from this data structure
// are not safe to modify without first copying them.
type LegacyData struct {
	data []byte
}

// Create a new LegacyData with the given value.
func NewLegacyData(value []byte) *LegacyData {
	data := make([]byte, legacyHeaderLength+len(value))
	copy(data[legacyValueStart:], value)
	return &LegacyData{data: data}
}

// Serialize the legacy data to a byte slice.
//
// The returned byte slice is not safe to modify without first copying it.
func (l *LegacyData) Serialize() []byte {
	return l.data
}

// Deserialize the legacy data from the given byte slice.
func DeserializeLegacyData(data []byte) (*LegacyData, error) {
	if len(data) == 0 {
		return nil, errors.New("data is empty")
	}

	legacyData := &LegacyData{
		data: data,
	}

	serializationVersion := legacyData.GetSerializationVersion()
	if serializationVersion != LegacyDataVersion0 {
		return nil, fmt.Errorf("unsupported serialization version: %d", serializationVersion)
	}

	if len(data) < legacyHeaderLength {
		return nil, fmt.Errorf("data length at version %d should be at least %d, got %d",
			serializationVersion, legacyHeaderLength, len(data))
	}

	return legacyData, nil
}

// Get the serialization version for this LegacyData instance.
func (l *LegacyData) GetSerializationVersion() LegacyDataVersion {
	return (LegacyDataVersion)(l.data[legacyVersionStart])
}

// Get the block height when this legacy data was last modified.
func (l *LegacyData) GetBlockHeight() uint64 {
	return binary.BigEndian.Uint64(l.data[legacyBlockHeightStart:legacyValueStart])
}

// Get the legacy value.
func (l *LegacyData) GetValue() []byte {
	return l.data[legacyValueStart:]
}

// Check if this legacy data signifies a deletion operation. A deletion operation is automatically
// performed when the value is empty (with the exception of the serialization version and block height).
func (l *LegacyData) IsDelete() bool {
	return len(l.data) == legacyHeaderLength
}

// Set the block height when this legacy data was last modified/touched. Returns self.
func (l *LegacyData) SetBlockHeight(blockHeight uint64) *LegacyData {
	binary.BigEndian.PutUint64(l.data[legacyBlockHeightStart:legacyValueStart], blockHeight)
	return l
}
