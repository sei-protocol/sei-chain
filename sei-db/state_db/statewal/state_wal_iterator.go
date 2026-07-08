package statewal

import (
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

var _ StateWALIterator = (*walIterator)(nil)

// walIterator adapts a generic seiwal iterator (which yields a block's changesets keyed by block number)
// into a StateWALIterator (which yields decoded block entries). Deserialization is handled by the underlying
// generic iterator; this wrapper only repackages each (index, changesets) pair as an *Entry.
type walIterator struct {
	inner seiwal.Iterator[[]*proto.NamedChangeSet]
	entry *Entry
}

// newStateIterator wraps a generic WAL iterator as a state WAL iterator.
func newStateIterator(inner seiwal.Iterator[[]*proto.NamedChangeSet]) *walIterator {
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
	index, changeset := it.inner.Entry()
	it.entry = &Entry{BlockNumber: index, Changeset: changeset}
	return true, nil
}

func (it *walIterator) Entry() *Entry {
	return it.entry
}

func (it *walIterator) Close() error {
	return it.inner.Close()
}
