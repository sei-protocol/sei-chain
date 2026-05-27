package pebbledb

import (
	"github.com/cockroachdb/pebble/v2"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

var _ dbm.Iterator = (*pebbleIterator)(nil)

// pebbleIterator implements dbm.Iterator over a Pebble iterator.
// Key/Value follow Pebble's zero-copy semantics; copy before modifying.
type pebbleIterator struct {
	it         *pebble.Iterator
	lowerBound []byte
	upperBound []byte
}

func newPebbleIterator(it *pebble.Iterator, opts *types.IterOptions) *pebbleIterator {
	pi := &pebbleIterator{it: it}
	if opts != nil {
		pi.lowerBound = opts.LowerBound
		pi.upperBound = opts.UpperBound
	}
	pi.it.First()
	return pi
}

func (pi *pebbleIterator) Domain() ([]byte, []byte) {
	return pi.lowerBound, pi.upperBound
}

func (pi *pebbleIterator) Valid() bool {
	return pi.it.Valid()
}

func (pi *pebbleIterator) Next() {
	if !pi.Valid() {
		return
	}
	pi.it.Next()
}

func (pi *pebbleIterator) Key() []byte {
	return pi.it.Key()
}

func (pi *pebbleIterator) Value() []byte {
	return pi.it.Value()
}

func (pi *pebbleIterator) Error() error {
	return pi.it.Error()
}

func (pi *pebbleIterator) Close() error {
	return pi.it.Close()
}
