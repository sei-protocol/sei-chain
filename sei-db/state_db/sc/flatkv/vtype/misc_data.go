package vtype

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type MiscDataVersion uint8

// DO NOT CHANGE VERSION VALUES!!! Adding new versions is ok, but historical versions should never be removed/changed.
const (
	MiscDataVersion0 MiscDataVersion = 0
)

/*
Serialization schema for MiscData version 0:

| Version | Block Height | Value    |
|---------|--------------|----------|
| 1 byte  | 8 bytes      | variable |

Data is stored in big-endian order. Value is variable length.
*/

const (
	miscVersionStart     = 0
	miscBlockHeightStart = miscVersionStart + VersionLength
	miscValueStart       = miscBlockHeightStart + BlockHeightLength
	miscHeaderLength     = VersionLength + BlockHeightLength
)

var _ VType = (*MiscData)(nil)

// Used for encapsulating and serializing misc data in the FlatKV misc database.
//
// This data structure is not threadsafe. Values passed into and values received from this data structure
// are not safe to modify without first copying them.
type MiscData struct {
	version     MiscDataVersion
	blockHeight int64
	value       []byte
	isDelete    bool
}

// Create a new MiscData with the given value.
func NewMiscData() *MiscData {
	return &MiscData{version: MiscDataVersion0}
}

// Serialize the misc data to a byte slice.
func (l *MiscData) Serialize() []byte {
	if l == nil {
		return make([]byte, miscHeaderLength)
	}
	data := make([]byte, miscHeaderLength+len(l.value))
	data[miscVersionStart] = byte(l.version)
	binary.BigEndian.PutUint64(data[miscBlockHeightStart:miscValueStart], uint64(l.blockHeight)) //nolint:gosec
	copy(data[miscValueStart:], l.value)
	return data
}

// Deserialize the misc data from the given byte slice.
func DeserializeMiscData(data []byte) (*MiscData, error) {
	if len(data) == 0 {
		return nil, errors.New("data is empty")
	}

	version := MiscDataVersion(data[miscVersionStart])
	if version != MiscDataVersion0 {
		return nil, fmt.Errorf("unsupported serialization version: %d", version)
	}

	if len(data) < miscHeaderLength {
		return nil, fmt.Errorf("data length at version %d should be at least %d, got %d",
			version, miscHeaderLength, len(data))
	}

	value := make([]byte, len(data)-miscHeaderLength)
	copy(value, data[miscValueStart:])

	return &MiscData{
		version:     version,
		blockHeight: int64(binary.BigEndian.Uint64(data[miscBlockHeightStart:miscValueStart])), //nolint:gosec
		value:       value,
	}, nil
}

// Get the serialization version for this MiscData instance.
func (l *MiscData) GetSerializationVersion() MiscDataVersion {
	if l == nil {
		return MiscDataVersion0
	}
	return l.version
}

// Get the block height when this misc entry was last modified.
func (l *MiscData) GetBlockHeight() int64 {
	if l == nil {
		return 0
	}
	return l.blockHeight
}

// Get the misc value.
func (l *MiscData) GetValue() []byte {
	if l == nil {
		return []byte{}
	}
	return l.value
}

// Set the block height when this misc entry was last modified/touched. Returns self (or a new MiscData if nil).
func (l *MiscData) SetBlockHeight(blockHeight int64) *MiscData {
	if l == nil {
		l = NewMiscData()
	}
	l.blockHeight = blockHeight
	return l
}

// Set the misc value. Returns self (or a new MiscData if nil).
// Clears the delete flag — an explicit SetValue is a write, not a deletion,
// even when value is empty ([]byte{} is a valid Cosmos module value).
func (l *MiscData) SetValue(value []byte) *MiscData {
	if l == nil {
		l = NewMiscData()
	}
	l.value = append([]byte(nil), value...)
	l.isDelete = false
	return l
}

// MarkDeleted flags this entry for physical key removal at commit time.
// The stored value is irrelevant once marked; IsDelete() will return true.
func (l *MiscData) MarkDeleted() *MiscData {
	if l == nil {
		l = NewMiscData()
	}
	l.isDelete = true
	return l
}

// IsDelete reports whether this entry represents a deletion.
// Uses an explicit flag rather than value-length inference so that empty
// values ([]byte{}) written by Cosmos modules are not misinterpreted as
// deletions.
func (l *MiscData) IsDelete() bool {
	if l == nil {
		return true
	}
	return l.isDelete
}
