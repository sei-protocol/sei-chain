package types

import (
	"encoding/binary"
	"fmt"
)

/*
Every value a Mutation holds is a serialized row, which begins with the same header whatever the row's
type:

| Version | Block Height |
|---------|--------------|
| 1 byte  | 8 bytes      |

The block height is big-endian. The payload that follows is specific to the row's type.
*/

const (
	rowVersionStart     = 0
	rowBlockHeightStart = 1
	rowHeaderLength     = 9

	// The only row header version.
	rowHeaderVersion0 = 0
)

// Mutation is one key's change in a block: its new value and the value it held before. A Mutation is
// immutable, and so are the bytes it holds.
type Mutation struct {
	// The key that changed.
	key string

	// The key's value in this block, or nil if this block deleted it.
	value []byte

	// The key's value before this block, or nil if it was absent.
	previous []byte
}

// NewMutation returns a Mutation.
func NewMutation(
	// The key that changed.
	key string,
	// The key's new value, or nil for a deletion.
	value []byte,
	// The key's value before the change, or nil if it was absent.
	previous []byte,
) Mutation {
	return Mutation{key: key, value: value, previous: previous}
}

// WithPrevious returns a copy of the Mutation whose previous value is previous.
func (m Mutation) WithPrevious(previous []byte) Mutation {
	m.previous = previous
	return m
}

// Key returns the key that changed.
func (m Mutation) Key() string {
	return m.key
}

// Value returns the key's value in this block, or nil if this block deleted it.
func (m Mutation) Value() []byte {
	return m.value
}

// Previous returns the key's value before this block, or nil if it was absent.
func (m Mutation) Previous() []byte {
	return m.previous
}

// BlockHeight returns the block height stamped in Value(), reporting false if this block deleted the key.
func (m Mutation) BlockHeight() (uint64, bool, error) {
	return rowBlockHeight(m.value)
}

// PreviousBlockHeight returns the block height stamped in Previous(), which is the height at which the key
// was last modified, reporting false if the key was absent.
func (m Mutation) PreviousBlockHeight() (uint64, bool, error) {
	return rowBlockHeight(m.previous)
}

// Payload returns Value() without its row header, or nil if this block deleted the key.
func (m Mutation) Payload() ([]byte, error) {
	return rowPayload(m.value)
}

// PreviousPayload returns Previous() without its row header, or nil if the key was absent.
func (m Mutation) PreviousPayload() ([]byte, error) {
	return rowPayload(m.previous)
}

// rowBlockHeight returns the block height stamped in row, reporting false for a nil row.
func rowBlockHeight(row []byte) (uint64, bool, error) {
	if row == nil {
		return 0, false, nil
	}
	if err := validateRowHeader(row); err != nil {
		return 0, false, fmt.Errorf("read block height: %w", err)
	}
	return binary.BigEndian.Uint64(row[rowBlockHeightStart:rowHeaderLength]), true, nil
}

// rowPayload returns row without its header, or nil for a nil row.
func rowPayload(row []byte) ([]byte, error) {
	if row == nil {
		return nil, nil
	}
	if err := validateRowHeader(row); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}
	return row[rowHeaderLength:], nil
}

// validateRowHeader returns an error if row is shorter than its header or has an unknown header version.
func validateRowHeader(row []byte) error {
	if len(row) < rowHeaderLength {
		return fmt.Errorf("row is %d bytes, shorter than its %d byte header", len(row), rowHeaderLength)
	}
	if version := row[rowVersionStart]; version != rowHeaderVersion0 {
		return fmt.Errorf("unsupported row header version: %d", version)
	}
	return nil
}
