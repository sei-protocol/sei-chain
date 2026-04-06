package types

import (
	"encoding/binary"
	"fmt"
)

// AddressLength is the length of a serialized address, in bytes.
const AddressLength = 9

// Address describes the location of data on disk.
type Address struct { // TODO before merge: consider folding in value size to this... I think it always goes together.
	// The segment index.
	index uint32
	// The byte offset of the data within the segment.
	offset uint32
	// The shard.
	shard uint8
}

// NewAddress creates a new address
func NewAddress(
	index uint32,
	offset uint32,
	shard uint8,
) Address {
	return Address{
		index:  index,
		offset: offset,
		shard:  shard,
	}
}

// DeserializeAddress converts a byte slice to an address.
func DeserializeAddress(bytes []byte) (Address, error) {
	if len(bytes) != AddressLength {
		var zero Address
		return zero, fmt.Errorf("invalid address length: %d", len(bytes))
	}
	return Address{
		index:  binary.BigEndian.Uint32(bytes[0:4]),
		offset: binary.BigEndian.Uint32(bytes[4:8]),
		shard:  bytes[8],
	}, nil
}

// Index returns the file index of the value address.
func (a Address) Index() uint32 {
	return a.index
}

// Shard returns the shard of the value address.
func (a Address) Shard() uint8 {
	return a.shard
}

// Offset returns the offset of the value address.
func (a Address) Offset() uint32 {
	return a.offset
}

// String returns a string representation of the address.
func (a Address) String() string {
	return fmt.Sprintf("(%d:%d:%d)", a.index, a.offset, a.shard)
}

// Serialize converts the address to a byte slice.
func (a Address) Serialize() []byte {
	bytes := make([]byte, AddressLength)
	binary.BigEndian.PutUint32(bytes[0:4], a.index)
	binary.BigEndian.PutUint32(bytes[4:8], a.offset)
	bytes[8] = a.shard
	return bytes
}
