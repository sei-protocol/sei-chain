package pebbledb

import (
	"fmt"

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

func (p *pebbleDB) NewBatch() types.Batch {
	return &pebbleBatch{b: p.db.NewBatch(), operationMetrics: p.operationMetrics}
}

func (pb *pebbleBatch) Set(key, value []byte) error {
	return pb.b.Set(key, value, nil)
}

func (pb *pebbleBatch) Delete(key []byte) error {
	return pb.b.Delete(key, nil)
}

func (pb *pebbleBatch) Commit(opts types.WriteOptions) error {
	writeCount := int64(pb.b.Count())
	err := pb.b.Commit(toPebbleWriteOpts(opts))
	if err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
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
