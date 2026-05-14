//go:build littdb_wip

package types

import (
	"encoding/binary"
	"fmt"
)

// AddressSerializedSize is the on-disk size of a serialized Address in bytes.
// Layout: index(4) | offset(4) | shardID(1) | valueSize(4)
const AddressSerializedSize = 13

// Address describes the location of a value on disk.
//
// An Address identifies the file the value lives in (Index), the byte offset of the value's length prefix
// within that file (Offset), the shard within the segment that owns the value (ShardID), and the size of
// the value itself in bytes (ValueSize).
type Address struct {
	// index is the segment index that owns the value. Combined with the shardID, it identifies the value file
	// that contains the value's bytes.
	index uint32
	// offset is the byte position of the value's length prefix within the shard's value file. The value's
	// bytes immediately follow the 4-byte length prefix.
	offset uint32
	// shardID is the index of the shard within the segment that holds the value. Encoded as a single byte,
	// which caps the maximum sharding factor at 256.
	shardID uint8
	// valueSize is the length of the value in bytes (not counting the 4-byte length prefix on disk).
	valueSize uint32
}

// NewAddress creates a new Address.
func NewAddress(index uint32, offset uint32, shardID uint8, valueSize uint32) Address {
	return Address{
		index:     index,
		offset:    offset,
		shardID:   shardID,
		valueSize: valueSize,
	}
}

// DeserializeAddress converts a byte slice to an Address. The slice must be exactly AddressSerializedSize bytes.
func DeserializeAddress(bytes []byte) (Address, error) {
	if len(bytes) != AddressSerializedSize {
		return Address{}, fmt.Errorf("invalid address length: %d", len(bytes))
	}
	return Address{
		index:     binary.BigEndian.Uint32(bytes[0:4]),
		offset:    binary.BigEndian.Uint32(bytes[4:8]),
		shardID:   bytes[8],
		valueSize: binary.BigEndian.Uint32(bytes[9:13]),
	}, nil
}

// Index returns the segment index of the value.
func (a Address) Index() uint32 {
	return a.index
}

// Offset returns the byte offset of the value within its shard's value file.
func (a Address) Offset() uint32 {
	return a.offset
}

// ShardID returns the shard within the segment that owns the value.
func (a Address) ShardID() uint8 {
	return a.shardID
}

// ValueSize returns the size of the value in bytes.
func (a Address) ValueSize() uint32 {
	return a.valueSize
}

// String returns a string representation of the address.
func (a Address) String() string {
	return fmt.Sprintf("(%d:%d@%d, %d)", a.index, a.offset, a.shardID, a.valueSize)
}

// Serialize converts the address to a byte slice of length AddressSerializedSize.
func (a Address) Serialize() []byte {
	bytes := make([]byte, AddressSerializedSize)
	binary.BigEndian.PutUint32(bytes[0:4], a.index)
	binary.BigEndian.PutUint32(bytes[4:8], a.offset)
	bytes[8] = a.shardID
	binary.BigEndian.PutUint32(bytes[9:13], a.valueSize)
	return bytes
}
