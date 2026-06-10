package pebbledb

import (
	"github.com/cockroachdb/pebble/v2"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// pebbleBatch wraps a Pebble batch for atomic writes.
// Important: Callers must call Close() after Commit() to release batch resources,
// even if Commit() succeeds. Failure to Close() will leak memory.
type pebbleBatch struct {
	b                *pebble.Batch
	operationMetrics *OperationMetrics
}

var _ types.Batch = (*pebbleBatch)(nil)

func newPebbleBatch(db *pebble.DB, operationMetrics *OperationMetrics) *pebbleBatch {
	return &pebbleBatch{b: db.NewBatch(), operationMetrics: operationMetrics}
}

func (p *pebbleDB) NewBatch() types.Batch {
	return newPebbleBatch(p.db, p.operationMetrics)
}

func (pb *pebbleBatch) Set(key, value []byte) error {
	// Durability options are applied on Commit.
	return pb.b.Set(key, value, nil)
}

func (pb *pebbleBatch) Delete(key []byte) error {
	// Durability options are applied on Commit.
	return pb.b.Delete(key, nil)
}

func (pb *pebbleBatch) Commit(opts types.WriteOptions) error {
	writeCount := int64(pb.b.Count())
	if err := pb.b.Commit(toPebbleWriteOpts(opts)); err != nil {
		return err
	}
	pb.operationMetrics.AddWrite(writeCount)
	return nil
}

func (pb *pebbleBatch) Len() int {
	return pb.b.Len()
}

func (pb *pebbleBatch) Reset() {
	pb.b.Reset()
}

func (pb *pebbleBatch) Close() error {
	return pb.b.Close()
}
