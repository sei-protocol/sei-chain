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
	it               *pebble.Iterator
	lowerBound       []byte
	upperBound       []byte
	reverse          bool
	operationMetrics *OperationMetrics
	readCount        int64
}

func newPebbleIterator(it *pebble.Iterator, opts *types.IterOptions, operationMetrics *OperationMetrics) *pebbleIterator {
	pi := &pebbleIterator{it: it, operationMetrics: operationMetrics}
	if opts != nil {
		pi.lowerBound = opts.LowerBound
		pi.upperBound = opts.UpperBound
		pi.reverse = opts.Reverse
	}
	if pi.reverse {
		pi.it.Last()
	} else {
		pi.it.First()
	}
	if pi.it.Valid() {
		pi.readCount++
	}
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
	if pi.reverse {
		pi.it.Prev()
	} else {
		pi.it.Next()
	}
	if pi.it.Valid() {
		pi.readCount++
	}
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
	if pi.operationMetrics != nil {
		pi.operationMetrics.AddRead(pi.readCount)
	}
	return pi.it.Close()
}
