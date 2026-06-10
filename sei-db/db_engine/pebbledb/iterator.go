package pebbledb

import (
	"github.com/cockroachdb/pebble/v2"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// pebbleIterator implements db_engine.Iterator using PebbleDB.
// Key/Value follow Pebble's zero-copy semantics; see db_engine.Iterator contract.
type pebbleIterator struct {
	it               *pebble.Iterator
	operationMetrics *OperationMetrics
	readCount        int64
}

var _ types.KeyValueDBIterator = (*pebbleIterator)(nil)

func (pi *pebbleIterator) record(valid bool) bool {
	if valid {
		pi.readCount++
	}
	return valid
}

func (pi *pebbleIterator) First() bool          { return pi.record(pi.it.First()) }
func (pi *pebbleIterator) Last() bool           { return pi.record(pi.it.Last()) }
func (pi *pebbleIterator) Valid() bool          { return pi.it.Valid() }
func (pi *pebbleIterator) SeekGE(k []byte) bool { return pi.record(pi.it.SeekGE(k)) }
func (pi *pebbleIterator) SeekLT(k []byte) bool { return pi.record(pi.it.SeekLT(k)) }
func (pi *pebbleIterator) Next() bool           { return pi.record(pi.it.Next()) }
func (pi *pebbleIterator) NextPrefix() bool     { return pi.record(pi.it.NextPrefix()) }
func (pi *pebbleIterator) Prev() bool           { return pi.record(pi.it.Prev()) }
func (pi *pebbleIterator) Key() []byte          { return pi.it.Key() }
func (pi *pebbleIterator) Value() []byte        { return pi.it.Value() }
func (pi *pebbleIterator) Error() error         { return pi.it.Error() }
func (pi *pebbleIterator) Close() error {
	pi.operationMetrics.AddRead(pi.readCount)
	return pi.it.Close()
}
