package multiversion

import "github.com/cosmos/cosmos-sdk/store/types"

// tracked iterator is a wrapper around an existing iterator to track the iterator progress and monitor which keys are iterated.
type trackedIterator struct {
	types.Iterator

	iterateset iterationTracker
	ReadsetHandler
	IterateSetHandler
}

// TODO: test

func NewTrackedIterator(iter types.Iterator, iterationTracker iterationTracker, iterateSetHandler IterateSetHandler, readSetHandler ReadsetHandler) *trackedIterator {
	return &trackedIterator{
		Iterator:          iter,
		iterateset:        iterationTracker,
		IterateSetHandler: iterateSetHandler,
		ReadsetHandler:    readSetHandler,
	}
}

// Close calls first updates the iterateset from the iterator, and then calls iterator.Close()
func (ti *trackedIterator) Close() error {
	// TODO: if there are more keys to the iterator, then we consider it early stopped?
	if ti.Iterator.Valid() {
		// TODO: test whether reaching end of iteration range means valid is true or false
		ti.iterateset.SetEarlyStopKey(ti.Iterator.Key())
	}
	// Update iterate set
	ti.IterateSetHandler.UpdateIterateSet(ti.iterateset)
	return ti.Iterator.Close()
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
