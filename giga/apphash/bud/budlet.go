package bud

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// Budlet is the 4-tuple (key, value, previous value, not-modified-since height) for one key a block modified: a
// leaf of that block's BUD tree.
type Budlet struct {
	// The live state database key the block modified. Never empty.
	key []byte

	// The payload the block wrote to the key.
	value []byte

	// The payload this budlet proves the key held at every height in [notModifiedSince, block height).
	previousValue []byte

	// The inclusive lower bound of the heights at which this budlet proves the key held previousValue.
	notModifiedSince uint64
}

// NewBudlet returns the budlet for one key a block modified, or an error if a parameter is out of bounds.
func NewBudlet(
	// The live state database key the block modified. Must not be empty or longer than 2^32-1 bytes. Retained,
	// so it must not be mutated afterward.
	key []byte,
	// The payload the block wrote to the key. Must not be longer than 2^32-1 bytes. Retained, so it must not be
	// mutated afterward.
	value []byte,
	// The payload this budlet proves the key held at every height in [notModifiedSince, block height). Must not
	// be longer than 2^32-1 bytes. Retained, so it must not be mutated afterward.
	previousValue []byte,
	// The inclusive lower bound of the heights at which this budlet proves the key held previousValue. Must be
	// below the block's height.
	notModifiedSince uint64,
) (*Budlet, error) {
	budlet := &Budlet{key: key, value: value, previousValue: previousValue, notModifiedSince: notModifiedSince}
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

// Key returns the live state database key the block modified. The caller must not mutate it.
func (b *Budlet) Key() []byte {
	return b.key
}

// Value returns the payload the block wrote to the key. The caller must not mutate it.
func (b *Budlet) Value() []byte {
	return b.value
}

// PreviousValue returns the payload the budlet proves the key held at every height in [NotModifiedSince(), block
// height). The caller must not mutate it.
func (b *Budlet) PreviousValue() []byte {
	return b.previousValue
}

// NotModifiedSince returns the inclusive lower bound of the heights at which the budlet proves the key held
// PreviousValue().
func (b *Budlet) NotModifiedSince() uint64 {
	return b.notModifiedSince
}

// Serialize returns the budlet's serialization, which is also the input to its BUD tree leaf hash. The
// serialization carries no version; it is defined by the BUD version.
func (b *Budlet) Serialize() []byte {
	return b.appendSerialized(make([]byte, 0, b.serializedSize()))
}

// serializedSize returns the length of the budlet's serialization.
func (b *Budlet) serializedSize() int {
	return 4 + len(b.key) + 4 + len(b.value) + 4 + len(b.previousValue) + 8
}

// appendSerialized appends the budlet's serialization to dst and returns the extended slice.
func (b *Budlet) appendSerialized(
	// The bytes to append to.
	dst []byte,
) []byte {
	dst = appendBudletField(dst, b.key)
	dst = appendBudletField(dst, b.value)
	dst = appendBudletField(dst, b.previousValue)
	return binary.BigEndian.AppendUint64(dst, b.notModifiedSince)
}

// appendBudletField appends the serialization of a budlet's key, value, or previous value: its length, then its
// bytes.
func appendBudletField(
	// The bytes to append to.
	serialized []byte,
	// The field to append.
	field []byte,
) []byte {
	fieldLength := uint32(len(field)) //nolint:gosec // G115 - the constructors bound the length
	serialized = binary.BigEndian.AppendUint32(serialized, fieldLength)
	return append(serialized, field...)
}

// readBudlet parses the serialized budlet at the start of data, returning it and the bytes after it.
func readBudlet(
	// Bytes that start with a serialized budlet.
	data []byte,
) (*Budlet, []byte, error) {
	key, data, err := readBudletField(data)
	if err != nil {
		return nil, nil, fmt.Errorf("reading key: %w", err)
	}
	value, data, err := readBudletField(data)
	if err != nil {
		return nil, nil, fmt.Errorf("reading value: %w", err)
	}
	previousValue, data, err := readBudletField(data)
	if err != nil {
		return nil, nil, fmt.Errorf("reading previous value: %w", err)
	}

	if len(data) < 8 {
		return nil, nil, fmt.Errorf("serialized budlet ends before its not-modified-since height")
	}
	notModifiedSince := binary.BigEndian.Uint64(data)
	rest := data[8:]

	budlet, err := NewBudlet(key, value, previousValue, notModifiedSince)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid budlet: %w", err)
	}
	return budlet, rest, nil
}

// readBudletField parses the serialized key, value, or previous value at the start of data, returning a copy of it
// and the bytes after it. The copy is never nil, even when empty.
func readBudletField(
	// Bytes that start with a serialized budlet field.
	data []byte,
) ([]byte, []byte, error) {
	if len(data) < 4 {
		return nil, nil, fmt.Errorf("serialized budlet ends before a field length")
	}
	fieldLength := uint64(binary.BigEndian.Uint32(data))
	data = data[4:]
	if uint64(len(data)) < fieldLength {
		return nil, nil, fmt.Errorf("serialized budlet ends inside a %d byte field", fieldLength)
	}
	return append([]byte{}, data[:fieldLength]...), data[fieldLength:], nil
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
