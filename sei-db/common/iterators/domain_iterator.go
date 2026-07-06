package iterators

import (
	"fmt"

	dbm "github.com/tendermint/tm-db"
)

var _ dbm.Iterator = (*domainIterator)(nil)

// domainIterator wraps a parent iterator and overrides Domain() to report a
// caller-supplied [start, end) range. It is useful when the parent is built
// over a physical/translated keyspace (so its own Domain() reflects physical
// bounds) but callers expect the logical bounds they requested, as required by
// the dbm.Iterator contract. All other methods are inherited from the parent.
type domainIterator struct {
	dbm.Iterator
	start []byte
	end   []byte
}

// NewDomainIterator returns an iterator that behaves exactly like parent except
// that Domain() reports [start, end). The parent must be non-nil.
func NewDomainIterator(parent dbm.Iterator, start, end []byte) (dbm.Iterator, error) {
	if parent == nil {
		return nil, fmt.Errorf("nil parent iterator")
	}
	return &domainIterator{Iterator: parent, start: start, end: end}, nil
}

func (d *domainIterator) Domain() ([]byte, []byte) {
	return d.start, d.end
}
