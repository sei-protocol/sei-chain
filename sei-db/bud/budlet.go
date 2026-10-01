package bud

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// Budlet is the triple (key, value, previous height) for one key a block modified: a leaf of that block's BUD
// tree.
type Budlet struct {
	// The key modified. Never empty.
	key []byte

	// The value written, or nil for a deletion.
	value []byte

	// The height of the block that last modified the key before this one, or 0 if no block did.
	previousHeight uint64
}

// NewBudlet returns the budlet for one key a block modified, or an error if a parameter is out of bounds.
func NewBudlet(
	// The key modified. Must not be empty or longer than 2^32-1 bytes. Retained, so it must not be mutated
	// afterward.
	key []byte,
	// The value written, or nil for a deletion; a non-nil empty value is a write of the empty value. Must not be
	// longer than 2^32-1 bytes. Retained, so it must not be mutated afterward.
	value []byte,
	// The height of the block that last modified key before this one, or 0 if no block did.
	previousHeight uint64,
) (*Budlet, error) {
	if len(key) == 0 {
		return nil, fmt.Errorf("creating budlet: key is empty")
	}
	if uint64(len(key)) > math.MaxUint32 {
		return nil, fmt.Errorf("creating budlet: key is %d bytes, more than %d",
			len(key), uint64(math.MaxUint32))
	}
	if uint64(len(value)) > math.MaxUint32 {
		return nil, fmt.Errorf("creating budlet: value is %d bytes, more than %d",
			len(value), uint64(math.MaxUint32))
	}
	return &Budlet{key: key, value: value, previousHeight: previousHeight}, nil
}

// DeserializeBudlet parses a budlet from its serialization. It returns an error unless data is exactly one
// serialized budlet that NewBudlet() would accept. The serialization carries no version:
// the caller takes it from the BUD version of the BUD tree or BUD proof the budlet came with.
func DeserializeBudlet(data []byte) (*Budlet, error) {
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

// Value returns the value the block wrote, or nil for a deletion. The caller must not mutate it.
func (b *Budlet) Value() []byte {
	return b.value
}

// PreviousHeight returns the height of the block that last modified the key before this one, or 0 if no block
// did.
func (b *Budlet) PreviousHeight() uint64 {
	return b.previousHeight
}

// Serialize returns the budlet's serialization, which is also the input to its BUD tree leaf hash. The
// serialization carries no version; it is defined by the BUD version.
func (b *Budlet) Serialize() []byte {
	keyLength := uint32(len(b.key))     //nolint:gosec // G115 - the constructors bound the length
	valueLength := uint32(len(b.value)) //nolint:gosec // G115 - the constructors bound the length

	serialized := make([]byte, 0, 4+len(b.key)+1+4+len(b.value)+8)
	serialized = binary.BigEndian.AppendUint32(serialized, keyLength)
	serialized = append(serialized, b.key...)
	if b.value == nil {
		serialized = append(serialized, 1)
	} else {
		serialized = append(serialized, 0)
	}
	serialized = binary.BigEndian.AppendUint32(serialized, valueLength)
	serialized = append(serialized, b.value...)
	serialized = binary.BigEndian.AppendUint64(serialized, b.previousHeight)
	return serialized
}

// readBudlet parses the serialized budlet at the start of data, returning it and the bytes after it.
func readBudlet(data []byte) (*Budlet, []byte, error) {
	if len(data) < 4 {
		return nil, nil, fmt.Errorf("serialized budlet is %d bytes, too short to hold a key length", len(data))
	}
	keyLength := uint64(binary.BigEndian.Uint32(data))
	data = data[4:]

	if uint64(len(data)) < keyLength+1+4 {
		return nil, nil, fmt.Errorf("serialized budlet ends before the fields after its %d byte key", keyLength)
	}
	key := bytes.Clone(data[:keyLength])
	deletionFlag := data[keyLength]
	valueLength := uint64(binary.BigEndian.Uint32(data[keyLength+1:]))
	data = data[keyLength+1+4:]

	if uint64(len(data)) < valueLength+8 {
		return nil, nil, fmt.Errorf("serialized budlet ends inside its %d byte value or previous height",
			valueLength)
	}
	previousHeight := binary.BigEndian.Uint64(data[valueLength:])
	rest := data[valueLength+8:]

	var value []byte
	switch deletionFlag {
	case 0:
		// Never nil, even when empty, so that an empty write is not read back as a deletion.
		value = append([]byte{}, data[:valueLength]...)
	case 1:
		if valueLength != 0 {
			return nil, nil, fmt.Errorf("serialized deletion budlet has a %d byte value", valueLength)
		}
	default:
		return nil, nil, fmt.Errorf("serialized budlet has deletion flag %d, want 0 or 1", deletionFlag)
	}
	budlet, err := NewBudlet(key, value, previousHeight)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid budlet: %w", err)
	}
	return budlet, rest, nil
}

// checkBudletsSortedByKey returns an error unless the budlets' keys are strictly increasing.
func checkBudletsSortedByKey(budlets []*Budlet) error {
	for i := 1; i < len(budlets); i++ {
		if bytes.Compare(budlets[i-1].key, budlets[i].key) >= 0 {
			return fmt.Errorf("budlet %d: key %x does not follow key %x",
				i, budlets[i].key, budlets[i-1].key)
		}
	}
	return nil
}
