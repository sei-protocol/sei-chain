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
	commitMetrics    *CommitMetrics
}

var _ types.Batch = (*pebbleBatch)(nil)

func (p *pebbleDB) NewBatch() types.Batch {
	return p.wrapBatch(p.db.NewBatch())
}

func (p *pebbleDB) NewBatchWithSize(size int) types.Batch {
	return p.wrapBatch(p.db.NewBatchWithSize(size))
}

// wrapBatch hands a pebble batch the metrics its operations and commits report through.
func (p *pebbleDB) wrapBatch(b *pebble.Batch) types.Batch {
	return &pebbleBatch{
		b:                b,
		operationMetrics: p.operationMetrics,
		commitMetrics:    p.commitMetrics,
	}
}

func (pb *pebbleBatch) Set(key, value []byte) error {
	return pb.b.Set(key, value, nil)
}

func (pb *pebbleBatch) Delete(key []byte) error {
	return pb.b.Delete(key, nil)
}

// Written through pebble's deferred-op API, which reserves the record inside the batch's own buffer
// and hands back slices to fill: copying a string into those needs no intermediate byte slice, and
// so no allocation per key.
func (pb *pebbleBatch) SetString(key string, value []byte) error {
	op := pb.b.SetDeferred(len(key), len(value))
	copy(op.Key, key)
	copy(op.Value, value)
	return op.Finish()
}

// Written through pebble's deferred-op API for the same reason as SetString.
func (pb *pebbleBatch) DeleteString(key string) error {
	op := pb.b.DeleteDeferred(len(key))
	copy(op.Key, key)
	return op.Finish()
}

func (pb *pebbleBatch) Append(other types.Batch) error {
	otherBatch, ok := other.(*pebbleBatch)
	if !ok {
		return fmt.Errorf("cannot append a %T to a pebble batch", other)
	}
	// Moves the other batch's encoded records in bulk rather than replaying them one at a time.
	if err := pb.b.Apply(otherBatch.b, nil); err != nil {
		return fmt.Errorf("failed to append batch: %w", err)
	}
	return nil
}

func (pb *pebbleBatch) Commit(opts types.WriteOptions) error {
	writeCount := int64(pb.b.Count())
	err := pb.b.Commit(toPebbleWriteOpts(opts))
	if err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}
	pb.operationMetrics.AddWrite(writeCount)
	// Read after the commit returns, since that is when pebble has finished filling the stats in.
	pb.commitMetrics.Record(pb.b.CommitStats())
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
