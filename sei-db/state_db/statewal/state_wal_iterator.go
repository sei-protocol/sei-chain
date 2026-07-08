package statewal

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

var _ StateWALIterator = (*walIterator)(nil)

// walIterator adapts a generic seiwal.Iterator (which yields opaque byte payloads keyed by index) into a
// StateWALIterator (which yields decoded block entries). Each record's index is the block number and its
// payload is the block's serialized changesets; deserialization happens in Next so it can surface an error,
// and the decoded entry is cached for Entry.
type walIterator struct {
	inner seiwal.Iterator
	entry *Entry
}

// newStateIterator wraps a generic WAL iterator as a state WAL iterator.
func newStateIterator(inner seiwal.Iterator) *walIterator {
	return &walIterator{inner: inner}
}

func (it *walIterator) Next() (bool, error) {
	ok, err := it.inner.Next()
	if err != nil {
		it.entry = nil
		return false, err
	}
	if !ok {
		it.entry = nil
		return false, nil
	}
	index, data := it.inner.Entry()
	changeset, err := deserializeChangesets(data)
	if err != nil {
		it.entry = nil
		return false, fmt.Errorf("failed to deserialize block %d: %w", index, err)
	}
	it.entry = &Entry{BlockNumber: index, Changeset: changeset}
	return true, nil
}

func (it *walIterator) Entry() *Entry {
	return it.entry
}

func (it *walIterator) Close() error {
	return it.inner.Close()
}
