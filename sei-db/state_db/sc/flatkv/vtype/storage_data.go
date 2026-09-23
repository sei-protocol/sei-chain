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

var _ VType = (*StorageData)(nil)

// Used for encapsulating and serializing storage slot data in the FlatKV storage database.
//
// This data structure is not threadsafe. Values passed into and values received from this data structure
// are not safe to modify without first copying them.
type StorageData struct {
	// data is the serialized form.
	data [storageDataLength]byte

	// valueZero reports whether the value is all 0s, which is what IsDelete answers. Maintained by
	// every method that writes the value region, and computed once at deserialization.
	//
	// Held here rather than derived on demand because the callers that ask are far from the ones that
	// write: by then the bytes have left the cache, and reading them back costs around forty times
	// what checking them at the point of the write does.
	valueZero bool
}

// Create a new StorageData initialized to all 0s.
func NewStorageData() *StorageData {
	return &StorageData{valueZero: true}
}

// NewStorageDataFrom returns the storage data for a raw 32-byte slot value written at blockHeight.
// rawValue is copied, so the caller may reuse it.
func NewStorageDataFrom(blockHeight int64, rawValue []byte) (*StorageData, error) {
	if len(rawValue) != StorageValueLength {
		return nil, fmt.Errorf("invalid storage value length: got %d, expected %d",
			len(rawValue), StorageValueLength)
	}
	storageData := &StorageData{}
	storageData.data[storageVersionStart] = byte(StorageDataVersion0)
	heightBytes := storageData.data[storageBlockHeightStart:storageValueStart]
	binary.BigEndian.PutUint64(heightBytes, uint64(blockHeight)) //nolint:gosec // height is non-negative
	copy(storageData.data[storageValueStart:], rawValue)
	// Read back out of the destination, which the copy above just left in cache, rather than out of
	// rawValue.
	storageData.valueZero = isZero(storageData.data[storageValueStart:])
	return storageData, nil
}

// Serialize the storage data to a byte slice.
//
// The returned byte slice is not safe to modify without first copying it.
func (s *StorageData) Serialize() []byte {
	if s == nil {
		return make([]byte, storageDataLength)
	}
	return s.data[:]
}

// Deserialize the storage data from the given byte slice.
func DeserializeStorageData(data []byte) (*StorageData, error) {
	if len(data) == 0 {
		return nil, errors.New("data is empty")
	}

	// Length is checked before any field is read: the fixed-size buffer cannot hold a short input,
	// so reading the version out of one would be out of bounds.
	if len(data) != storageDataLength {
		return nil, fmt.Errorf("data length should be %d, got %d", storageDataLength, len(data))
	}

	storageData := &StorageData{}
	copy(storageData.data[:], data)

	serializationVersion := storageData.GetSerializationVersion()
	if serializationVersion != StorageDataVersion0 {
		return nil, fmt.Errorf("unsupported serialization version: %d", serializationVersion)
	}

	storageData.valueZero = isZero(storageData.data[storageValueStart:])
	return storageData, nil
}

// Get the serialization version for this StorageData instance.
func (s *StorageData) GetSerializationVersion() StorageDataVersion {
	if s == nil {
		return StorageDataVersion0
	}
	return (StorageDataVersion)(s.data[storageVersionStart])
}

// Get the block height when this storage slot was last modified.
func (s *StorageData) GetBlockHeight() int64 {
	if s == nil {
		return 0
	}
	heightBytes := s.data[storageBlockHeightStart:storageValueStart]
	return int64(binary.BigEndian.Uint64(heightBytes)) //nolint:gosec // height fits in int64
}

// Get the storage slot value.
func (s *StorageData) GetValue() *[32]byte {
	if s == nil {
		var zero [32]byte
		return &zero
	}
	return (*[32]byte)(s.data[storageValueStart:])
}

// Check if this storage data signifies a deletion operation. A deletion operation is automatically
// performed when the value is all 0s (with the exception of the serialization version and block height).
func (s *StorageData) IsDelete() bool {
	if s == nil {
		return true
	}
	return s.valueZero
}

// Set the block height when this storage slot was last modified/touched. Returns self (or a new StorageData if nil).
func (s *StorageData) SetBlockHeight(blockHeight int64) *StorageData {
	if s == nil {
		s = NewStorageData()
	}
	heightBytes := s.data[storageBlockHeightStart:storageValueStart]
	binary.BigEndian.PutUint64(heightBytes, uint64(blockHeight)) //nolint:gosec // height is non-negative
	return s
}

// Set the storage slot value. Returns self (or a new StorageData if nil).
func (s *StorageData) SetValue(value *[32]byte) *StorageData {
	if s == nil {
		s = NewStorageData()
	}
	if value == nil {
		var zero [32]byte
		value = &zero
	}
	copy(s.data[storageValueStart:storageDataLength], value[:])
	s.valueZero = *value == [StorageValueLength]byte{}
	return s
}
