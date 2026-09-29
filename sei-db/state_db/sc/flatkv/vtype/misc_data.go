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

// MiscData is a row in the FlatKV misc database. An empty value is a value rather than a deletion, since
// []byte{} is a valid Cosmos module value.
//
// The value is shared rather than copied when a MiscData is copied, and must not be mutated.
type MiscData struct {
	// The block height at which this entry was last modified.
	blockHeight uint64

	// The entry's value.
	value []byte
}

// NewMiscData returns a new MiscData with every field zero.
func NewMiscData() *MiscData {
	return &MiscData{}
}

// AppendMiscData appends the serialized form of misc to dst and returns the extended slice.
func AppendMiscData(dst []byte, misc MiscData) []byte {
	dst = append(dst, byte(MiscDataVersion0))
	dst = binary.BigEndian.AppendUint64(dst, misc.blockHeight)
	return append(dst, misc.value...)
}

// Serialize returns the serialized form of the entry in a new slice.
func (l MiscData) Serialize() []byte {
	return AppendMiscData(make([]byte, 0, miscHeaderLength+len(l.value)), l)
}

// DeserializeMiscData parses a misc entry from its serialized form. The returned value aliases data, and
// is non-nil even when empty.
func DeserializeMiscData(data []byte) (MiscData, error) {
	if len(data) == 0 {
		return MiscData{}, errors.New("data is empty")
	}

	version := MiscDataVersion(data[miscVersionStart])
	if version != MiscDataVersion0 {
		return MiscData{}, fmt.Errorf("unsupported serialization version: %d", version)
	}
	if len(data) < miscHeaderLength {
		return MiscData{}, fmt.Errorf("data length at version %d should be at least %d, got %d",
			version, miscHeaderLength, len(data))
	}

	return MiscData{
		blockHeight: binary.BigEndian.Uint64(data[miscBlockHeightStart:miscValueStart]),
		value:       data[miscValueStart:],
	}, nil
}

// GetBlockHeight returns the block height at which the entry was last modified.
func (l MiscData) GetBlockHeight() uint64 {
	return l.blockHeight
}

// GetValue returns the entry's value, which must not be mutated.
func (l MiscData) GetValue() []byte {
	return l.value
}

// SetBlockHeight sets the block height at which the entry was last modified. Returns the receiver.
func (l *MiscData) SetBlockHeight(blockHeight uint64) *MiscData {
	l.blockHeight = blockHeight
	return l
}

// SetValue sets the entry's value to a copy of value, which is non-nil even when empty. Returns the
// receiver.
func (l *MiscData) SetValue(value []byte) *MiscData {
	l.value = append(make([]byte, 0, len(value)), value...)
	return l
}
