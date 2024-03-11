package multiversion

import (
	"github.com/cosmos/cosmos-sdk/store/types"
)

// tracked iterator is a wrapper around an existing iterator to track the iterator progress and monitor which keys are iterated.
type trackedIterator struct {
	types.Iterator

	iterateset *iterationTracker
}

func NewTrackedIterator(iter types.Iterator, iterationTracker *iterationTracker) *trackedIterator {
	return &trackedIterator{
		Iterator:   iter,
		iterateset: iterationTracker,
	}
}

func (ti *trackedIterator) Valid() bool {
	valid := ti.Iterator.Valid()
	// if no longer valid, remove the early stop key since we reached end of range
	if !valid {
		ti.iterateset.SetEarlyStopKey(nil)
	} else {
		key := ti.Iterator.Key()
		ti.iterateset.AddKey(key)
	}
	return valid
}

// Key calls the iterator.Key() and adds the key to the iterateset, then returns the key from the iterator
func (ti *trackedIterator) Key() []byte {
	key := ti.Iterator.Key()
	// add key to the tracker
	ti.iterateset.AddKey(key)
	return key
}

// Value calls the iterator.Key() and adds the key to the iterateset, then returns the value from the iterator
func (ti *trackedIterator) Value() []byte {
	key := ti.Iterator.Key()
	val := ti.Iterator.Value()
	// add key to the tracker
	ti.iterateset.AddKey(key)
	return val
}

func (ti *trackedIterator) Next() {
	// add current key to the tracker
	key := ti.Iterator.Key()
	ti.iterateset.AddKey(key)
	// call next
	ti.Iterator.Next()
}
