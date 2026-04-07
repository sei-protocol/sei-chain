package types

import (
	"encoding/binary"
	"fmt"
)

// AddressLength is the length of a serialized address, in bytes.
const AddressLength = 13

// Address describes the location of data on disk.
type Address struct { // TODO before merge: consider folding in value size to this... I think it always goes together.
	// The segment index.
	index uint32
	// The byte offset of the data within the segment.
	offset uint32
	// The shard.
	shard uint8
	// The size of the value, in bytes.
	valueSize uint32
}

// NewAddress creates a new address
func NewAddress(
	index uint32,
	offset uint32,
	shard uint8,
	valueSize uint32,
) Address {
	return Address{
		index:     index,
		offset:    offset,
		shard:     shard,
		valueSize: valueSize,
	}
}

// DeserializeAddress converts a byte slice to an address.
func DeserializeAddress(bytes []byte) (Address, error) {
	if len(bytes) != AddressLength {
		var zero Address
		return zero, fmt.Errorf("invalid address length: %d", len(bytes))
	}
	return Address{
		index:     binary.BigEndian.Uint32(bytes[0:4]),
		offset:    binary.BigEndian.Uint32(bytes[4:8]),
		shard:     bytes[8],
		valueSize: binary.BigEndian.Uint32(bytes[9:13]),
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

// Get the size of the value, in bytes.
func (a Address) ValueSize() uint32 {
	return a.valueSize
}

// String returns a string representation of the address.
func (a Address) String() string {
	return fmt.Sprintf("(%d:%d:%d:%d)", a.index, a.offset, a.shard, a.valueSize)
}

// Serialize converts the address to a newly allocated byte slice.
func (a Address) Serialize() []byte {
	buf := make([]byte, AddressLength)
	a.SerializeInto(buf)
	return buf
}

// SerializeInto writes the serialized address into the provided buffer.
// The buffer must be at least AddressLength bytes long.
func (a Address) SerializeInto(buf []byte) {
	binary.BigEndian.PutUint32(buf[0:4], a.index)
	binary.BigEndian.PutUint32(buf[4:8], a.offset)
	buf[8] = a.shard
	binary.BigEndian.PutUint32(buf[9:13], a.valueSize)
}
