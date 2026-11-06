package vtype

import (
	"encoding/binary"
	"fmt"
)

// valueHeaderLength is the length of the serialization version and block height that begin every serialized VType.
const valueHeaderLength = VersionLength + BlockHeightLength

// SplitValue separates a serialized VType of any type into its serialization version, the height of the block that
// wrote it, and its remaining data. It returns an error if value is too short to hold a serialization version and a
// block height.
func SplitValue(
	// The serialized VType.
	value []byte,
) (
	// The serialization version.
	version byte,
	// The height of the block that wrote the value.
	blockHeight uint64,
	// Everything after the block height, opaque to this function. Aliases value.
	data []byte,
	// Any error encountered while splitting the value.
	err error,
) {
	if len(value) < valueHeaderLength {
		return 0, 0, nil, fmt.Errorf("value is %d bytes, shorter than its %d byte header",
			len(value), valueHeaderLength)
	}
	return value[0], binary.BigEndian.Uint64(value[VersionLength:valueHeaderLength]), value[valueHeaderLength:], nil
}
