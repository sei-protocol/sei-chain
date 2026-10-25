package memiavl

import (
	"bytes"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	dbm "github.com/tendermint/tm-db"
)

var _ dbm.Iterator = (*Iterator)(nil)

type Iterator struct {
	// domain of iteration, end is exclusive
	start, end []byte
	ascending  bool
	zeroCopy   bool

	// snapshot is the snapshot backing the nodes this iterator walks, held for the
	// iterator's lifetime and released by Close. nil when no snapshot backs them.
	snapshot *Snapshot

	// cache the next key-value pair
	key, value []byte

	valid bool

	stack []Node
}

// NewIterator returns an iterator over the tree rooted at root, backed by
// snapshot, which is nil when no snapshot backs those nodes. The iterator holds a
// reference on snapshot until Close.
func NewIterator(start, end []byte, ascending bool, root Node, zeroCopy bool, snapshot *Snapshot) *Iterator {
	// A persisted node's key and value are slices into this mmap, and both Key and
	// Value read them after the tree's read lock is gone, so the reference is what
	// stops a concurrent rewrite unmapping the region mid-iteration. That unmap
	// surfaces as a fatal fault in runtime.memmove, which no caller can recover.
	if snapshot != nil {
		snapshot.Acquire()
	}
	iter := &Iterator{
		start:     start,
		end:       end,
		ascending: ascending,
		valid:     true,
		zeroCopy:  zeroCopy,
		snapshot:  snapshot,
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
		return utils.Clone(iter.key)
	}
	return iter.key
}

// Value implements dbm.Iterator
func (iter *Iterator) Value() []byte {
	if !iter.zeroCopy {
		return utils.Clone(iter.value)
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
	// Under zeroCopy these still point into the mapping the release below may
	// unmap, so drop them with the reference rather than leaving a caller able to
	// reach freed pages through a closed iterator.
	iter.key = nil
	iter.value = nil

	snapshot := iter.snapshot
	iter.snapshot = nil
	if snapshot == nil {
		return nil
	}
	return snapshot.Close()
}
