package pebbledb

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cockroachdb/pebble/v2"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// writePipeline applies committed batches to a pebble database in the order they were committed.
type writePipeline struct {
	// The database batches are written to.
	db *pebble.DB

	// Cancelled by Close().
	ctx context.Context

	// Cancels ctx.
	cancel context.CancelFunc

	// The pool each batch's ordering runs on.
	cpuPool threading.Pool

	// Committed batches awaiting their write, in commit order.
	writeQueue chan *commitHandle

	// Closed by the writer as it exits.
	writerDone chan struct{}

	// Counters for the logical writes each batch carries.
	operationMetrics *OperationMetrics

	// Timings pebble reports for each commit.
	commitMetrics *CommitMetrics
}

var _ types.CommitHandle = (*commitHandle)(nil)

// commitHandle is one committed batch moving through the pipeline.
type commitHandle struct {
	// The batch's writes, ordered by key once sorted is closed.
	entries []batchEntry

	// The durability the batch was committed with.
	opts types.WriteOptions

	// The pipeline's context, set by Submit().
	ctx context.Context

	// Closed once entries are ordered.
	sorted chan struct{}

	// Closed once the batch has been applied.
	done chan struct{}

	// The error applying the batch produced, readable once done is closed.
	err error
}

// errPipelineClosed is reported for a batch the pipeline will not apply because it is shutting down.
var errPipelineClosed = errors.New("database is shutting down")

// newWritePipeline starts a pipeline against db. The caller closes it.
func newWritePipeline(
	// The parent of the pipeline's own context.
	ctx context.Context,
	// The database batches are written to.
	db *pebble.DB,
	// A pool for work that is CPU bound and does no IO.
	cpuPool threading.Pool,
	// How many committed batches may await their write before Submit() blocks.
	queueSize int,
	// Counters for the logical writes each batch carries.
	operationMetrics *OperationMetrics,
	// Timings pebble reports for each commit.
	commitMetrics *CommitMetrics,
) *writePipeline {
	pipelineCtx, cancel := context.WithCancel(ctx)
	p := &writePipeline{
		db:               db,
		ctx:              pipelineCtx,
		cancel:           cancel,
		cpuPool:          cpuPool,
		writeQueue:       make(chan *commitHandle, queueSize),
		writerDone:       make(chan struct{}),
		operationMetrics: operationMetrics,
		commitMetrics:    commitMetrics,
	}

	go p.write()

	return p
}

// Submit queues a committed batch for writing, blocking while the queue is full. It reports an
// error for a batch the pipeline will not apply, having begun shutting down.
func (p *writePipeline) Submit(handle *commitHandle) error {
	handle.ctx = p.ctx

	// Ahead of the queue, so that the ordering overlaps the wait for room in it.
	p.cpuPool.Submit(func() {
		sortHandle(handle)
	})

	if err := threading.InterruptiblePush(p.ctx, p.writeQueue, handle); err != nil {
		return errPipelineClosed
	}
	return nil
}

// Close shuts the pipeline down. It returns once none of the pipeline's goroutines can still touch
// the database.
func (p *writePipeline) Close() {
	p.cancel()

	// The writer may be inside a pebble commit, which has to finish before the database closes.
	<-p.writerDone
}

// write applies batches in commit order until the pipeline is cancelled.
func (p *writePipeline) write() {
	defer close(p.writerDone)

	for {
		handle, err := threading.InterruptiblePull(p.ctx, p.writeQueue)
		if err != nil {
			return
		}
		handle.err = p.completeHandle(handle)
		close(handle.done)
	}
}

// completeHandle writes a handle's entries once they are ordered, or abandons the batch when
// shutdown overtakes its ordering.
func (p *writePipeline) completeHandle(handle *commitHandle) error {
	// Waiting on this batch rather than on whichever is ordered first is what holds the write order
	// to the commit order.
	select {
	case <-handle.sorted:
		return p.writeHandle(handle)
	case <-p.ctx.Done():
		return errPipelineClosed
	}
}

// writeHandle applies one ordered batch to pebble.
func (p *writePipeline) writeHandle(handle *commitHandle) error {
	// Pebble's pooled batch still carries the buffer its last use grew; asking for a sized one
	// replaces that buffer with a fresh allocation.
	batch := p.db.NewBatch()
	defer func() {
		_ = batch.Close()
	}()

	for _, entry := range handle.entries {
		if entry.deleted {
			if err := deleteString(batch, entry.key); err != nil {
				return fmt.Errorf("failed to write a delete: %w", err)
			}
			continue
		}
		if err := setString(batch, entry.key, entry.value); err != nil {
			return fmt.Errorf("failed to write a set: %w", err)
		}
	}

	writeCount := int64(batch.Count())
	if err := batch.Commit(toPebbleWriteOpts(handle.opts)); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}
	p.operationMetrics.AddWrite(writeCount)
	// Read after the commit returns, since that is when pebble has finished filling the stats in.
	p.commitMetrics.Record(batch.CommitStats())

	return nil
}

// sortHandle orders a batch's entries by key and marks it ordered.
func sortHandle(handle *commitHandle) {
	// Ordering is what makes the entries cheap for pebble to absorb: its memtable caches the splice
	// it last inserted at. Bytewise to match the comparer Open() pins, and stable so that two writes
	// to one key reach pebble in the order they were collected.
	slices.SortStableFunc(handle.entries, func(a batchEntry, b batchEntry) int {
		return strings.Compare(a.key, b.key)
	})
	close(handle.sorted)
}

// Wait blocks until the batch has been applied and reports what applying it produced. It reports
// that the database is shutting down for a batch that shutdown abandoned.
func (h *commitHandle) Wait() error {
	select {
	case <-h.done:
		return h.err
	case <-h.ctx.Done():
		return errPipelineClosed
	}
}

// setString adds a set of key to value, copying the key without an intermediate byte slice.
func setString(batch *pebble.Batch, key string, value []byte) error {
	op := batch.SetDeferred(len(key), len(value))
	copy(op.Key, key)
	copy(op.Value, value)
	return op.Finish()
}

// deleteString adds a delete of key, copying it without an intermediate byte slice.
func deleteString(batch *pebble.Batch, key string) error {
	op := batch.DeleteDeferred(len(key))
	copy(op.Key, key)
	return op.Finish()
}
