package view

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// Mutation is one key's change in a sealed version: its new value and the value it held before. A
// Mutation is immutable, and so are the bytes it holds.
type Mutation struct {
	// The key that changed.
	key string

	// The key's value in this version, or nil if this version deleted it.
	value []byte

	// The key's value in the version before, or nil if it was absent.
	previous []byte
}

// NewMutation returns a Mutation for a change produced outside a view.
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

// Key returns the key that changed.
func (m Mutation) Key() string {
	return m.key
}

// Value returns the key's value in this version, or nil if this version deleted it.
func (m Mutation) Value() []byte {
	return m.value
}

// Previous returns the key's value in the version before, or nil if it was absent.
func (m Mutation) Previous() []byte {
	return m.previous
}

// BlockHeight returns the block height stamped in Value(), reporting false if this version deleted the key.
func (m Mutation) BlockHeight() (uint64, bool, error) {
	return rowBlockHeight(m.value)
}

// PreviousBlockHeight returns the block height stamped in Previous(), which is the height at which the key
// was last modified, reporting false if the key was absent.
func (m Mutation) PreviousBlockHeight() (uint64, bool, error) {
	return rowBlockHeight(m.previous)
}

// Payload returns Value() without its row header, or nil if this version deleted the key.
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
	height, err := vtype.RowBlockHeight(row)
	if err != nil {
		return 0, false, fmt.Errorf("read block height: %w", err)
	}
	return height, true, nil
}

// rowPayload returns row without its header, or nil for a nil row.
func rowPayload(row []byte) ([]byte, error) {
	if row == nil {
		return nil, nil
	}
	payload, err := vtype.RowPayload(row)
	if err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}
	return payload, nil
}
