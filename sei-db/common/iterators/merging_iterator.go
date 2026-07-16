package iterators

import (
	"bytes"
	"fmt"

	dbm "github.com/tendermint/tm-db"
)

var _ dbm.Iterator = (*mergingIterator)(nil)

// mergingIterator merges multiple iterators into a single iterator.
//
// Each child must be in ascending lexicographic order and must not emit
// duplicate keys; if either assumption is violated, behavior is undefined.
//
// Output is in global lexicographic order. When multiple children share the
// same key, that key is emitted once; the rightmost child (highest argument
// index) supplies the value and lower-index copies are dropped.
type mergingIterator struct {
	// the nested iterators to combine
	iterators []dbm.Iterator

	// union of child start domains, fixed at construction
	start []byte

	// union of child end domains, fixed at construction
	end []byte

	// the index of the iterator that should next emit a value
	nextIteratorIndex int

	// ascending is true when children iterate in ascending key order.
	ascending bool

	// the error encountered by the iterator, if any
	err error
}

// NewMergingIterator combines iterators into a single iterator.
//
// Each child must iterate in the same direction as ascending (lex ascending
// when true, lex descending when false) without duplicate keys; otherwise
// behavior is undefined. Duplicate keys across children are emitted once; the
// last child wins.
//
// Intended for a small number of iterators (on the order of half a dozen). May
// not be performant for combining large numbers of iterators.
func NewMergingIterator(ascending bool, iterators ...dbm.Iterator) (dbm.Iterator, error) {
	m := &mergingIterator{
		iterators:         make([]dbm.Iterator, len(iterators)),
		nextIteratorIndex: -1,
		ascending:         ascending,
	}
	copy(m.iterators, iterators)

	for i, child := range m.iterators {
		if child == nil {
			_ = m.Close()
			return nil, fmt.Errorf("nil iterator at index %d", i)
		}
		if err := child.Error(); err != nil {
			_ = m.Close()
			return nil, fmt.Errorf("error in iterator at index %d: %w", i, err)
		}
	}

	m.start, m.end = mergeDomain(m.iterators)
	m.findNext()
	return m, nil
}

func (m *mergingIterator) findNext() {
	if m.ascending {
		m.findMin()
	} else {
		m.findMax()
	}
}

// findMin sets nextIteratorIndex to the valid child with the smallest current
// key, breaking ties toward the highest index. Child errors are checked here
// and cached on the merge iterator via fail.
func (m *mergingIterator) findMin() {
	if m.err != nil {
		return
	}
	m.nextIteratorIndex = -1
	var smallestKey []byte
	for i, child := range m.iterators {
		if child == nil {
			continue
		}
		if err := child.Error(); err != nil {
			m.fail(err)
			return
		}
		if !child.Valid() {
			continue
		}
		childKey := child.Key()
		if m.nextIteratorIndex < 0 {
			m.nextIteratorIndex = i
			smallestKey = bytes.Clone(childKey)
			continue
		}
		cmp := bytes.Compare(childKey, smallestKey)
		if cmp < 0 || (cmp == 0 && i > m.nextIteratorIndex) {
			m.nextIteratorIndex = i
			smallestKey = bytes.Clone(childKey)
		}
	}
}

// findMax sets nextIteratorIndex to the valid child with the largest current
// key, breaking ties toward the highest index.
func (m *mergingIterator) findMax() {
	if m.err != nil {
		return
	}
	m.nextIteratorIndex = -1
	var largestKey []byte
	for i, child := range m.iterators {
		if child == nil {
			continue
		}
		if err := child.Error(); err != nil {
			m.fail(err)
			return
		}
		if !child.Valid() {
			continue
		}
		childKey := child.Key()
		if m.nextIteratorIndex < 0 {
			m.nextIteratorIndex = i
			largestKey = bytes.Clone(childKey)
			continue
		}
		cmp := bytes.Compare(childKey, largestKey)
		if cmp > 0 || (cmp == 0 && i > m.nextIteratorIndex) {
			m.nextIteratorIndex = i
			largestKey = bytes.Clone(childKey)
		}
	}
}

// advanceChildrenAtKey advances every child positioned at key past that key.
func (m *mergingIterator) advanceChildrenAtKey(key []byte) {
	for _, child := range m.iterators {
		if child == nil {
			continue
		}
		if err := child.Error(); err != nil {
			m.fail(err)
			return
		}
		if !child.Valid() || !bytes.Equal(child.Key(), key) {
			continue
		}
		child.Next()
		if err := child.Error(); err != nil {
			m.fail(err)
			return
		}
	}
}

// fail records the first error, closes all children, and clears iterators so no
// further child methods are invoked.
func (m *mergingIterator) fail(err error) {
	if m.err != nil {
		return
	}
	m.err = err
	m.nextIteratorIndex = -1
	for _, child := range m.iterators {
		if child != nil {
			_ = child.Close()
		}
	}
	m.iterators = nil
}

func (m *mergingIterator) Close() error {
	if m.iterators == nil {
		return nil
	}
	var firstErr error
	for _, child := range m.iterators {
		if child == nil {
			continue
		}
		if err := child.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	m.iterators = nil
	m.nextIteratorIndex = -1
	return firstErr
}

func (m *mergingIterator) Domain() ([]byte, []byte) {
	return m.start, m.end
}

// mergeDomain returns the union of child iterator domains: the smallest lower
// bound and the largest upper bound.
func mergeDomain(iters []dbm.Iterator) (start, end []byte) {
	first := true
	for _, child := range iters {
		if child == nil {
			continue
		}
		childStart, childEnd := child.Domain()
		if first {
			start, end = childStart, childEnd
			first = false
			continue
		}
		start = minStart(start, childStart)
		end = maxEnd(end, childEnd)
	}
	return start, end
}

// minStart returns the smaller of two inclusive-lower iterator bounds. A nil
// bound means unbounded and wins over any non-nil bound.
func minStart(left, right []byte) []byte {
	if left == nil || right == nil {
		return nil
	}
	if bytes.Compare(left, right) <= 0 {
		return left
	}
	return right
}

// maxEnd returns the larger of two exclusive-upper iterator bounds (upper
// bounds are exclusive). A nil bound means unbounded and wins over any non-nil
// bound.
func maxEnd(left, right []byte) []byte {
	if left == nil || right == nil {
		return nil
	}
	if bytes.Compare(left, right) >= 0 {
		return left
	}
	return right
}

func (m *mergingIterator) Error() error {
	return m.err
}

func (m *mergingIterator) Key() []byte {
	if !m.Valid() {
		return nil
	}
	return m.iterators[m.nextIteratorIndex].Key()
}

func (m *mergingIterator) Next() {
	if !m.Valid() {
		return
	}

	currentKey := bytes.Clone(m.iterators[m.nextIteratorIndex].Key())
	m.advanceChildrenAtKey(currentKey)
	if m.err != nil {
		return
	}
	m.findNext()
}

func (m *mergingIterator) Valid() bool {
	return m.nextIteratorIndex >= 0 && m.err == nil
}

func (m *mergingIterator) Value() []byte {
	if !m.Valid() {
		return nil
	}
	return m.iterators[m.nextIteratorIndex].Value()
}
