package bud

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// Budlet is the 4-tuple (key, value, previous value, anchor height) for one key a block modified: a leaf of that
// block's BUD tree.
type Budlet struct {
	// The key modified. Never empty.
	key []byte

	// The key's value after the block, or nil if the key is absent after the block.
	value []byte

	// The value this budlet proves the key held at every height in [anchorHeight, block height), or nil if the key
	// was absent there.
	previousValue []byte

	// The inclusive lower bound of the heights at which this budlet proves the key held previousValue.
	anchorHeight uint64
}

// NewBudlet returns the budlet for one key a block modified, or an error if a parameter is out of bounds.
func NewBudlet(
	// The key modified. Must not be empty or longer than 2^32-1 bytes. Retained, so it must not be mutated
	// afterward.
	key []byte,
	// The key's value after the block, or nil if the key is absent after the block; a non-nil empty value is the
	// empty value. Must not be longer than 2^32-1 bytes. Retained, so it must not be mutated afterward.
	value []byte,
	// The value this budlet proves the key held at every height in [anchorHeight, block height), or nil if the key
	// was absent there; a non-nil empty value is the empty value. Must not be longer than 2^32-1 bytes. Retained,
	// so it must not be mutated afterward.
	previousValue []byte,
	// The inclusive lower bound of the heights at which this budlet proves the key held previousValue. Must be
	// below the block's height.
	anchorHeight uint64,
) (*Budlet, error) {
	budlet := &Budlet{key: key, value: value, previousValue: previousValue, anchorHeight: anchorHeight}
	if err := budlet.validate(); err != nil {
		return nil, fmt.Errorf("creating budlet: %w", err)
	}
	return budlet, nil
}

// DeserializeBudlet parses a budlet from its serialization. It returns an error unless data is exactly one
// serialized budlet that NewBudlet() would accept. The serialization carries no version:
// the caller takes it from the BUD version of the BUD tree or BUD proof the budlet came with.
func DeserializeBudlet(
	// The serialized budlet.
	data []byte,
) (*Budlet, error) {
	budlet, rest, err := readBudlet(data)
	if err != nil {
		return nil, fmt.Errorf("deserializing budlet: %w", err)
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("serialized budlet is followed by %d more bytes", len(rest))
	}
	return budlet, nil
}

// Key returns the key the block modified. The caller must not mutate it.
func (b *Budlet) Key() []byte {
	return b.key
}

// Value returns the key's value after the block, or nil if the key is absent after the block. The caller must not
// mutate it.
func (b *Budlet) Value() []byte {
	return b.value
}

// PreviousValue returns the value the budlet proves the key held at every height in [AnchorHeight(), block height),
// or nil if the key was absent there. The caller must not mutate it.
func (b *Budlet) PreviousValue() []byte {
	return b.previousValue
}

// AnchorHeight returns the inclusive lower bound of the heights at which the budlet proves the key held
// PreviousValue().
func (b *Budlet) AnchorHeight() uint64 {
	return b.anchorHeight
}

// Serialize returns the budlet's serialization, which is also the input to its BUD tree leaf hash. The
// serialization carries no version; it is defined by the BUD version.
func (b *Budlet) Serialize() []byte {
	keyLength := uint32(len(b.key)) //nolint:gosec // G115 - the constructors bound the length

	serialized := make([]byte, 0, 4+len(b.key)+1+4+len(b.value)+1+4+len(b.previousValue)+8)
	serialized = binary.BigEndian.AppendUint32(serialized, keyLength)
	serialized = append(serialized, b.key...)
	serialized = appendBudletValue(serialized, b.value)
	serialized = appendBudletValue(serialized, b.previousValue)
	serialized = binary.BigEndian.AppendUint64(serialized, b.anchorHeight)
	return serialized
}

// appendBudletValue appends the serialization of a budlet value: a deletion flag, a length, and the value's bytes.
func appendBudletValue(
	// The bytes to append to.
	serialized []byte,
	// The value, or nil for an absent key.
	value []byte,
) []byte {
	if value == nil {
		serialized = append(serialized, 1)
	} else {
		serialized = append(serialized, 0)
	}
	valueLength := uint32(len(value)) //nolint:gosec // G115 - the constructors bound the length
	serialized = binary.BigEndian.AppendUint32(serialized, valueLength)
	return append(serialized, value...)
}

// readBudlet parses the serialized budlet at the start of data, returning it and the bytes after it.
func readBudlet(
	// Bytes that start with a serialized budlet.
	data []byte,
) (*Budlet, []byte, error) {
	if len(data) < 4 {
		return nil, nil, fmt.Errorf("serialized budlet is %d bytes, too short to hold a key length", len(data))
	}
	keyLength := uint64(binary.BigEndian.Uint32(data))
	data = data[4:]
	if uint64(len(data)) < keyLength {
		return nil, nil, fmt.Errorf("serialized budlet ends inside its %d byte key", keyLength)
	}
	key := bytes.Clone(data[:keyLength])
	data = data[keyLength:]

	value, data, err := readBudletValue(data)
	if err != nil {
		return nil, nil, fmt.Errorf("reading value: %w", err)
	}
	previousValue, data, err := readBudletValue(data)
	if err != nil {
		return nil, nil, fmt.Errorf("reading previous value: %w", err)
	}

	if len(data) < 8 {
		return nil, nil, fmt.Errorf("serialized budlet ends before its anchor height")
	}
	anchorHeight := binary.BigEndian.Uint64(data)
	rest := data[8:]

	budlet, err := NewBudlet(key, value, previousValue, anchorHeight)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid budlet: %w", err)
	}
	return budlet, rest, nil
}

// readBudletValue parses the serialized budlet value at the start of data, returning it and the bytes after it. A
// deletion is returned as nil, and a written value as non-nil, even when empty.
func readBudletValue(
	// Bytes that start with a serialized budlet value.
	data []byte,
) ([]byte, []byte, error) {
	if len(data) < 1+4 {
		return nil, nil, fmt.Errorf("serialized budlet ends before a deletion flag and value length")
	}
	deletionFlag := data[0]
	valueLength := uint64(binary.BigEndian.Uint32(data[1:]))
	data = data[1+4:]
	if uint64(len(data)) < valueLength {
		return nil, nil, fmt.Errorf("serialized budlet ends inside a %d byte value", valueLength)
	}

	switch deletionFlag {
	case 0:
		// Never nil, even when empty, so that an empty value is not read back as a deletion.
		return append([]byte{}, data[:valueLength]...), data[valueLength:], nil
	case 1:
		if valueLength != 0 {
			return nil, nil, fmt.Errorf("serialized deletion has a %d byte value", valueLength)
		}
		return nil, data, nil
	default:
		return nil, nil, fmt.Errorf("serialized budlet has deletion flag %d, want 0 or 1", deletionFlag)
	}
}

// validate returns an error unless the budlet's key, value, and previous value are within the bounds NewBudlet()
// accepts.
func (b *Budlet) validate() error {
	if len(b.key) == 0 {
		return fmt.Errorf("key is empty")
	}
	if uint64(len(b.key)) > math.MaxUint32 {
		return fmt.Errorf("key is %d bytes, more than %d", len(b.key), uint64(math.MaxUint32))
	}
	if uint64(len(b.value)) > math.MaxUint32 {
		return fmt.Errorf("value is %d bytes, more than %d", len(b.value), uint64(math.MaxUint32))
	}
	if uint64(len(b.previousValue)) > math.MaxUint32 {
		return fmt.Errorf("previous value is %d bytes, more than %d",
			len(b.previousValue), uint64(math.MaxUint32))
	}
	return nil
}

// checkBudletsValid returns an error unless every budlet is non-nil and one NewBudlet() would accept.
func checkBudletsValid(
	// The budlets to check.
	budlets []*Budlet,
) error {
	for i, budlet := range budlets {
		if budlet == nil {
			return fmt.Errorf("budlet %d is nil", i)
		}
		if err := budlet.validate(); err != nil {
			return fmt.Errorf("budlet %d: %w", i, err)
		}
	}
	return nil
}

// checkBudletsSortedByKey returns an error unless the budlets' keys are strictly increasing.
func checkBudletsSortedByKey(
	// The budlets to check.
	budlets []*Budlet,
) error {
	for i := 1; i < len(budlets); i++ {
		if bytes.Compare(budlets[i-1].key, budlets[i].key) >= 0 {
			return fmt.Errorf("budlet %d: key %x does not follow key %x",
				i, budlets[i].key, budlets[i-1].key)
		}
	}
	return nil
}
