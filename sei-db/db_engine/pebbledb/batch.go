package pebbledb

import (
	"fmt"

	"github.com/cockroachdb/pebble/v2"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// pebbleBatch wraps a Pebble batch for atomic writes.
// Important: Callers must call Close() after Commit() to release batch resources,
// even if Commit() succeeds. Failure to Close() will leak memory.
type pebbleBatch struct {
	b                *pebble.Batch
	operationMetrics *OperationMetrics
	commitMetrics    *CommitMetrics

	// closed records whether Close has been called.
	closed utils.CloseMarker[pebbleBatch]
}

var _ types.Batch = (*pebbleBatch)(nil)

// NewBatch returns a batch from pebble's pool, which still carries the buffer its previous use grew.
// Asking pebble for a sized batch instead replaces that buffer with a fresh allocation on every call,
// which is why nothing here does.
func (p *pebbleDB) NewBatch() types.Batch {
	pb := &pebbleBatch{
		b:                p.db.NewBatch(),
		operationMetrics: p.operationMetrics,
		commitMetrics:    p.commitMetrics,
	}
	pb.closed = utils.MustClose(pb, "pebbledb batch")
	return pb
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
	pb.closed.Close(pb)
	return pb.b.Close()
}
