package vtype

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type StorageDataVersion uint8

// DO NOT CHANGE VERSION VALUES!!! Adding new versions is ok, but historical versions should never be removed/changed.
const (
	StorageDataVersion0 StorageDataVersion = 0
)

/*
Serialization schema for StorageData version 0:

| Version | Block Height | Value    |
|---------|--------------|----------|
| 1 byte  | 8 bytes      | 32 bytes |

Data is stored in big-endian order.
*/

const (
	storageVersionStart     = 0
	storageBlockHeightStart = storageVersionStart + VersionLength
	storageValueStart       = storageBlockHeightStart + BlockHeightLength

	storageDataLength = VersionLength + BlockHeightLength + StorageValueLength
)

// StorageData is a storage slot row in the FlatKV storage database. The zero value is a slot holding
// zero.
type StorageData struct {
	// The block height at which this slot was last modified.
	blockHeight uint64

	// The slot's value.
	value [StorageValueLength]byte
}

// NewStorageData returns a new StorageData with every field zero.
func NewStorageData() *StorageData {
	return &StorageData{}
}

// AppendStorageData appends the serialized form of storage to dst and returns the extended slice.
func AppendStorageData(dst []byte, storage StorageData) []byte {
	dst = append(dst, byte(StorageDataVersion0))
	dst = binary.BigEndian.AppendUint64(dst, storage.blockHeight)
	return append(dst, storage.value[:]...)
}

// Serialize returns the serialized form of the slot in a new slice.
func (s StorageData) Serialize() []byte {
	return AppendStorageData(make([]byte, 0, storageDataLength), s)
}

// DeserializeStorageData parses a storage slot from its serialized form.
func DeserializeStorageData(data []byte) (StorageData, error) {
	if len(data) == 0 {
		return StorageData{}, errors.New("data is empty")
	}

	version := StorageDataVersion(data[storageVersionStart])
	if version != StorageDataVersion0 {
		return StorageData{}, fmt.Errorf("unsupported serialization version: %d", version)
	}
	if len(data) != storageDataLength {
		return StorageData{}, fmt.Errorf("data length at version %d should be %d, got %d",
			version, storageDataLength, len(data))
	}

	return StorageData{
		blockHeight: binary.BigEndian.Uint64(data[storageBlockHeightStart:storageValueStart]),
		value:       [StorageValueLength]byte(data[storageValueStart:storageDataLength]),
	}, nil
}

// GetBlockHeight returns the block height at which the slot was last modified.
func (s StorageData) GetBlockHeight() uint64 {
	return s.blockHeight
}

// GetValue returns the slot's value.
func (s StorageData) GetValue() [StorageValueLength]byte {
	return s.value
}

// IsDelete reports whether the slot holds zero. The store deletes a slot that is set to zero.
func (s StorageData) IsDelete() bool {
	return s.value == [StorageValueLength]byte{}
}

// SetBlockHeight sets the block height at which the slot was last modified. Returns the receiver.
func (s *StorageData) SetBlockHeight(blockHeight uint64) *StorageData {
	s.blockHeight = blockHeight
	return s
}

// SetValue sets the slot's value. A nil value is all zeros. Returns the receiver.
func (s *StorageData) SetValue(value *[StorageValueLength]byte) *StorageData {
	if value == nil {
		s.value = [StorageValueLength]byte{}
	} else {
		s.value = *value
	}
	return s
}
