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
// are not safe to modify without first copying them. The value is held in its serialized form,
// with the delete flag beside it because deletion is not representable in that form.
type MiscData struct {
	data     []byte
	isDelete bool
}

// Create a new MiscData with an empty value.
func NewMiscData() *MiscData {
	return &MiscData{data: make([]byte, miscHeaderLength)}
}

// NewMiscDataFrom returns the misc data for value written at blockHeight, built directly in its
// serialized form so the value is copied once rather than once here and again at serialize time.
func NewMiscDataFrom(blockHeight int64, value []byte) *MiscData {
	data := make([]byte, miscHeaderLength+len(value))
	data[miscVersionStart] = byte(MiscDataVersion0)
	heightBytes := data[miscBlockHeightStart:miscValueStart]
	binary.BigEndian.PutUint64(heightBytes, uint64(blockHeight)) //nolint:gosec // height is non-negative
	copy(data[miscValueStart:], value)
	return &MiscData{data: data}
}

// NewDeletedMiscData returns misc data marking its key for removal at blockHeight.
func NewDeletedMiscData(blockHeight int64) *MiscData {
	return NewMiscDataFrom(blockHeight, nil).MarkDeleted()
}

// Serialize the misc data to a byte slice.
//
// The returned byte slice is not safe to modify without first copying it.
func (l *MiscData) Serialize() []byte {
	if l == nil {
		return make([]byte, miscHeaderLength)
	}
	return l.data
}

// Deserialize the misc data from the given byte slice.
//
// The returned MiscData owns its bytes; data may be reused or modified afterwards.
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

	// Copied rather than aliased: the caller's buffer is commonly borrowed from the storage engine
	// or an iterator, and GetValue hands out a subslice of whatever is held here.
	owned := make([]byte, len(data))
	copy(owned, data)
	return &MiscData{data: owned}, nil
}

// Get the serialization version for this MiscData instance.
func (l *MiscData) GetSerializationVersion() MiscDataVersion {
	if l == nil {
		return MiscDataVersion0
	}
	return MiscDataVersion(l.data[miscVersionStart])
}

// Get the block height when this misc entry was last modified.
func (l *MiscData) GetBlockHeight() int64 {
	if l == nil {
		return 0
	}
	heightBytes := l.data[miscBlockHeightStart:miscValueStart]
	return int64(binary.BigEndian.Uint64(heightBytes)) //nolint:gosec // height fits in int64
}

// Get the misc value.
func (l *MiscData) GetValue() []byte {
	if l == nil {
		return []byte{}
	}
	return l.data[miscValueStart:]
}

// Set the block height when this misc entry was last modified/touched. Returns self (or a new MiscData if nil).
func (l *MiscData) SetBlockHeight(blockHeight int64) *MiscData {
	if l == nil {
		l = NewMiscData()
	}
	heightBytes := l.data[miscBlockHeightStart:miscValueStart]
	binary.BigEndian.PutUint64(heightBytes, uint64(blockHeight)) //nolint:gosec // height is non-negative
	return l
}

// Set the misc value. Returns self (or a new MiscData if nil).
// Clears the delete flag — an explicit SetValue is a write, not a deletion,
// even when value is empty ([]byte{} is a valid Cosmos module value).
func (l *MiscData) SetValue(value []byte) *MiscData {
	if l == nil {
		l = NewMiscData()
	}
	next := make([]byte, miscHeaderLength+len(value))
	copy(next, l.data[:miscHeaderLength])
	copy(next[miscValueStart:], value)
	l.data = next
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
