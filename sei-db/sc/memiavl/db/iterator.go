package memiavl

import "bytes"

type Iterator struct {
	// domain of iteration, end is exclusive
	start, end []byte
	ascending  bool
	zeroCopy   bool

	// cache the next key-value pair
	key, value []byte

	valid bool

	stack []Node
}

func NewIterator(start, end []byte, ascending bool, root Node, zeroCopy bool) *Iterator {
	iter := &Iterator{
		start:     start,
		end:       end,
		ascending: ascending,
		valid:     true,
		zeroCopy:  zeroCopy,
	}

	if root != nil {
		iter.stack = []Node{root}
	}

	// cache the first key-value
	iter.Next()
	return iter
}

func (iter *Iterator) Domain() ([]byte, []byte) {
	return iter.start, iter.end
}

// Valid implements dbm.Iterator.
func (iter *Iterator) Valid() bool {
	return iter.valid
}

// Error implements dbm.Iterator
func (iter *Iterator) Error() error {
	return nil
}

// Key implements dbm.Iterator
func (iter *Iterator) Key() []byte {
	if !iter.zeroCopy {
		return bytes.Clone(iter.key)
	}
	return iter.key
}

// Value implements dbm.Iterator
func (iter *Iterator) Value() []byte {
	if !iter.zeroCopy {
		return bytes.Clone(iter.value)
	}
	return iter.value
}

// Next implements dbm.Iterator
func (iter *Iterator) Next() {
	for len(iter.stack) > 0 {
		// pop node
		node := iter.stack[len(iter.stack)-1]
		iter.stack = iter.stack[:len(iter.stack)-1]

		key := node.Key()
		startCmp := bytes.Compare(iter.start, key)
		afterStart := iter.start == nil || startCmp < 0
		beforeEnd := iter.end == nil || bytes.Compare(key, iter.end) < 0

		if node.IsLeaf() {
			startOrAfter := afterStart || startCmp == 0
			if startOrAfter && beforeEnd {
				iter.key = key
				iter.value = node.Value()
				return
			}
		} else {
			// push children to stack
			if iter.ascending {
				if beforeEnd {
					iter.stack = append(iter.stack, node.Right())
				}
				if afterStart {
					iter.stack = append(iter.stack, node.Left())
				}
			} else {
				if afterStart {
					iter.stack = append(iter.stack, node.Left())
				}
				if beforeEnd {
					iter.stack = append(iter.stack, node.Right())
				}
			}
		}
	}

	iter.valid = false
}

// Close implements dbm.Iterator
func (iter *Iterator) Close() error {
	iter.valid = false
	iter.stack = nil
	return nil
}
