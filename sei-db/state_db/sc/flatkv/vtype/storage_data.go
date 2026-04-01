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
	storageBlockHeightStart = 1
	storageValueStart       = 9
	storageDataLength       = 41
)

var _ VType = (*StorageData)(nil)

// Used for encapsulating and serializing storage slot data in the FlatKV storage database.
//
// This data structure is not threadsafe. Values passed into and values received from this data structure
// are not safe to modify without first copying them.
type StorageData struct {
	data []byte
}

// Create a new StorageData initialized to all 0s.
func NewStorageData() *StorageData {
	return &StorageData{
		data: make([]byte, storageDataLength),
	}
}

// Serialize the storage data to a byte slice.
//
// The returned byte slice is not safe to modify without first copying it.
func (s *StorageData) Serialize() []byte {
	if s == nil {
		return make([]byte, storageDataLength)
	}
	return s.data
}

// Deserialize the storage data from the given byte slice.
func DeserializeStorageData(data []byte) (*StorageData, error) {
	if len(data) == 0 {
		return nil, errors.New("data is empty")
	}

	storageData := &StorageData{
		data: data,
	}

	serializationVersion := storageData.GetSerializationVersion()
	if serializationVersion != StorageDataVersion0 {
		return nil, fmt.Errorf("unsupported serialization version: %d", serializationVersion)
	}

	if len(data) != storageDataLength {
		return nil, fmt.Errorf("data length at version %d should be %d, got %d",
			serializationVersion, storageDataLength, len(data))
	}

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
	return int64(binary.BigEndian.Uint64(s.data[storageBlockHeightStart:storageValueStart])) //nolint:gosec // block height is always within int64 range
}

// Get the storage slot value.
func (s *StorageData) GetValue() *[32]byte {
	if s == nil {
		var zero [32]byte
		return &zero
	}
	return (*[32]byte)(s.data[storageValueStart:storageDataLength])
}

// Check if this storage data signifies a deletion operation. A deletion operation is automatically
// performed when the value is all 0s (with the exception of the serialization version and block height).
func (s *StorageData) IsDelete() bool {
	if s == nil {
		return true
	}
	for i := storageValueStart; i < storageDataLength; i++ {
		if s.data[i] != 0 {
			return false
		}
	}
	return true
}

// Set the block height when this storage slot was last modified/touched. Returns self (or a new StorageData if nil).
func (s *StorageData) SetBlockHeight(blockHeight int64) *StorageData {
	if s == nil {
		s = NewStorageData()
	}
	binary.BigEndian.PutUint64(s.data[storageBlockHeightStart:storageValueStart], uint64(blockHeight)) //nolint:gosec // block height is always non-negative
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
	return s
}
