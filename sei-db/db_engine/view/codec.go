package view

import "fmt"

// Codec converts a ViewManager's values to and from the bytes its database stores.
//
// Decode(Append(nil, v)) must reproduce v, and Append(nil, Decode(b)) must reproduce b exactly: a manager
// hands its consumers values, and a consumer that needs the stored bytes back re-encodes them.
type Codec[V any] struct {
	// Appends the stored encoding of value to dst and returns the extended slice.
	Append func(dst []byte, value V) []byte

	// Returns the value data encodes. data belongs to the callee, which may retain it.
	Decode func(data []byte) (V, error)

	// Returns the number of bytes value is counted as toward the read cache's size budget.
	Size func(value V) uint64
}

// Validate returns an error if any of the codec's functions is missing.
func (c *Codec[V]) Validate() error {
	if c.Append == nil {
		return fmt.Errorf("codec has no Append function")
	}
	if c.Decode == nil {
		return fmt.Errorf("codec has no Decode function")
	}
	if c.Size == nil {
		return fmt.Errorf("codec has no Size function")
	}
	return nil
}

// encode returns the stored encoding of value in a fresh slice, which is never nil: a nil slice is a
// tombstone wherever bytes leave the manager.
func (c *Codec[V]) encode(value V) []byte {
	return c.Append(make([]byte, 0, c.Size(value)), value)
}

// optionalValue is a key's value, or the knowledge that it holds none. The zero optionalValue holds none.
//
// Where a map of writes holds one that is not present, it records a delete.
type optionalValue[V any] struct {
	// The value. The zero V when present is false.
	value V

	// Whether the key holds a value.
	present bool
}

// someValue returns an optionalValue holding value.
func someValue[V any](value V) optionalValue[V] {
	return optionalValue[V]{value: value, present: true}
}
