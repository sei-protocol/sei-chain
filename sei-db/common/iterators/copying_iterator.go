package iterators

import (
	"fmt"

	dbm "github.com/tendermint/tm-db"
)

var _ dbm.Iterator = (*copyingIterator)(nil)

// copyingIterator makes a parent iterator's Key/Value slices stable: at each
// position it eagerly copies both into freshly allocated buffers and serves
// those until the next advance.
//
// Pebble-backed iterators reuse their internal key/value buffers on Next, so
// any slice they return is silently rewritten as iteration advances. Callers
// throughout sei-cosmos retain iterator slices across Next and rely on them
// staying intact — cachekv keys its dirty-entry maps by unsafe zero-copy
// strings of the caller's slice, and the staking EndBlock queue processing
// does store.Delete(iter.Key()) mid-iteration — semantics that hold for
// memiavl/IAVL iterators (stable per-node slices). Serving reused buffers
// there corrupts the cache maps and silently drops the deletes from the
// block's changeset (nondeterministically across nodes, since the corruption
// depends on buffer reuse patterns). Wrapping the store's public iterators
// with this copier restores the stability those callers assume.
type copyingIterator struct {
	parent dbm.Iterator
	key    []byte
	value  []byte
}

// NewCopyingIterator wraps parent so that Key() and Value() return slices
// that remain valid until the iterator is closed, regardless of how the
// parent manages its buffers.
func NewCopyingIterator(parent dbm.Iterator) (dbm.Iterator, error) {
	if parent == nil {
		return nil, fmt.Errorf("nil parent iterator")
	}
	c := &copyingIterator{parent: parent}
	c.capture()
	return c, nil
}

// capture snapshots the parent's current entry into freshly allocated
// buffers. New allocations per position are the point: previously returned
// slices must never be overwritten by later positions.
func (c *copyingIterator) capture() {
	if !c.parent.Valid() {
		c.key = nil
		c.value = nil
		return
	}
	c.key = append([]byte(nil), c.parent.Key()...)
	c.value = append([]byte(nil), c.parent.Value()...)
}

func (c *copyingIterator) Domain() ([]byte, []byte) { return c.parent.Domain() }

func (c *copyingIterator) Valid() bool { return c.parent.Valid() }

func (c *copyingIterator) Next() {
	c.parent.Next()
	c.capture()
}

func (c *copyingIterator) Key() []byte {
	if !c.parent.Valid() {
		return nil
	}
	return c.key
}

func (c *copyingIterator) Value() []byte {
	if !c.parent.Valid() {
		return nil
	}
	return c.value
}

func (c *copyingIterator) Error() error { return c.parent.Error() }

func (c *copyingIterator) Close() error {
	c.key = nil
	c.value = nil
	return c.parent.Close()
}
