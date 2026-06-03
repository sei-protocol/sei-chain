package iterators

import (
	"fmt"

	dbm "github.com/tendermint/tm-db"
)

var _ dbm.Iterator = (*transformingIterator)(nil)

// A function used to transform key/value pairs returned by an iterator.
type IteratorTransform func(
	// The input key.
	inputKey []byte,
	// The input value.
	inputValue []byte,
) (
	// The resulting key to emit.
	outputKey []byte,
	// The resulting value to emit.
	outputValue []byte,
	// Whether to skip the current key/value pair. If true, the iterator will
	// not emit this key/value pair.
	skip bool,
	// An error to return if the transform fails (e.g. parsing failure)
	err error,
)

// transformingIterator applies a transform to each key/value pair from a parent
// iterator, optionally skipping entries.
type transformingIterator struct {
	// The parent iterator to transform.
	parent dbm.Iterator
	// The function used to transform key/value pairs.
	transform IteratorTransform
	// The next key/value pair to emit.
	key []byte
	// The next value to emit.
	value []byte
	// valid reports whether key/value hold a current entry to emit. Tracked
	// explicitly rather than inferred from key != nil so a transform may
	// legitimately emit a nil/empty key without terminating iteration.
	valid bool
	// The error encountered by the iterator, if any.
	err error
}

// NewTransformingIterator returns an iterator that emits transformed key/value pairs
// from parent, skipping pairs for which transform returns skip=true.
func NewTransformingIterator(parent dbm.Iterator, transform IteratorTransform) (dbm.Iterator, error) {
	if parent == nil {
		return nil, fmt.Errorf("nil parent iterator")
	}
	if transform == nil {
		_ = parent.Close()
		return nil, fmt.Errorf("nil transform")
	}
	if err := parent.Error(); err != nil {
		_ = parent.Close()
		return nil, fmt.Errorf("parent iterator error: %w", err)
	}
	m := &transformingIterator{
		parent:    parent,
		transform: transform,
	}
	m.advance()
	if err := m.Error(); err != nil {
		_ = m.Close()
		return nil, err
	}
	return m, nil
}

// advance moves to the next non-skipped parent entry, or clears the position if
// none remain.
func (m *transformingIterator) advance() {
	m.key = nil
	m.value = nil
	m.valid = false
	if m.parent == nil {
		return
	}
	for m.parent.Valid() {
		if err := m.parent.Error(); err != nil {
			m.fail(err)
			return
		}
		inputKey := m.parent.Key()
		inputValue := m.parent.Value()
		outputKey, outputValue, skip, err := m.transform(inputKey, inputValue)
		if err != nil {
			m.fail(err)
			return
		}
		if !skip {
			m.key = outputKey
			m.value = outputValue
			m.valid = true
			return
		}
		m.parent.Next()
	}
	if err := m.parent.Error(); err != nil {
		m.fail(err)
	}
}

// fail records the first error, closes the parent, and clears it so no further
// parent methods are invoked.
func (m *transformingIterator) fail(err error) {
	if m.err != nil {
		return
	}
	m.err = err
	m.key = nil
	m.value = nil
	m.valid = false
	if m.parent != nil {
		_ = m.parent.Close()
		m.parent = nil
	}
}

func (m *transformingIterator) Close() error {
	if m.parent == nil {
		return nil
	}
	err := m.parent.Close()
	m.parent = nil
	m.key = nil
	m.value = nil
	m.valid = false
	return err
}

func (m *transformingIterator) Domain() ([]byte, []byte) {
	if m.parent == nil {
		return nil, nil
	}
	return m.parent.Domain()
}

func (m *transformingIterator) Error() error {
	return m.err
}

func (m *transformingIterator) Key() []byte {
	if !m.Valid() {
		return nil
	}
	return m.key
}

func (m *transformingIterator) Next() {
	if !m.Valid() {
		return
	}
	m.parent.Next()
	if err := m.parent.Error(); err != nil {
		m.fail(err)
		return
	}
	m.advance()
}

func (m *transformingIterator) Valid() bool {
	return m.err == nil && m.valid
}

func (m *transformingIterator) Value() []byte {
	if !m.Valid() {
		return nil
	}
	return m.value
}
