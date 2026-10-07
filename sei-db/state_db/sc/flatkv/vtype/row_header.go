package vtype

import (
	"encoding/binary"
	"fmt"
)

/*
Every serialized row (account, storage, code and misc) begins with the same header:

| Version | Block Height |
|---------|--------------|
| 1 byte  | 8 bytes      |

The block height is big-endian. The payload that follows is specific to the row's type.
*/

const (
	rowVersionStart     = 0
	rowBlockHeightStart = rowVersionStart + VersionLength
	rowHeaderLength     = rowBlockHeightStart + BlockHeightLength

	// The only serialization version any row type has. Every type's version 0 uses the header above.
	rowHeaderVersion0 = 0
)

// RowBlockHeight returns the block height stamped in a serialized row of any type. It returns an error if the
// row is shorter than its header or has an unknown serialization version.
func RowBlockHeight(row []byte) (uint64, error) {
	if err := validateRowHeader(row); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(row[rowBlockHeightStart:rowHeaderLength]), nil
}

// RowPayload returns a serialized row of any type without its header. The result aliases row. It returns an
// error if the row is shorter than its header or has an unknown serialization version.
func RowPayload(row []byte) ([]byte, error) {
	if err := validateRowHeader(row); err != nil {
		return nil, err
	}
	return row[rowHeaderLength:], nil
}

// validateRowHeader returns an error if row is shorter than its header or has an unknown serialization
// version.
func validateRowHeader(row []byte) error {
	if len(row) < rowHeaderLength {
		return fmt.Errorf("row is %d bytes, shorter than its %d byte header", len(row), rowHeaderLength)
	}
	if version := row[rowVersionStart]; version != rowHeaderVersion0 {
		return fmt.Errorf("unsupported row serialization version: %d", version)
	}
	return nil
}
