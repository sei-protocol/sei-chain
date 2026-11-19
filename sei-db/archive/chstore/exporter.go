package chstore

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/sei-db/archive"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/clickhouse"
)

var logger = seilog.NewLogger("db", "archive")

const (
	// A batch is written once it holds maxBatchBlocks blocks, maxBatchTxs transactions,
	// maxBatchRows rows in one table or maxBatchBytes of variable-length data, and otherwise every
	// flushInterval.
	//
	// The archive is at most two batches behind execution, so a node can always roll back to it:
	// 2*maxBatchBlocks is within the storage's default rollback window of 1,000 blocks.
	maxBatchBlocks = 250
	maxBatchTxs    = 500_000
	maxBatchRows   = 1_000_000
	maxBatchBytes  = 256 << 20
	flushInterval  = time.Second
)

// Exporter writes every block the executor produces to the archive, in order and in batches. It
// copies each block into the current batch as it arrives and writes filled batches in the
// background.
type Exporter struct {
	writer *writer
	// Two batches take turns: one fills while the other is written. full hands the filled one to
	// writeBatches, and free returns it written and reset.
	full      chan *batch
	free      chan *batch
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once

	// writeErr is the first failed write. Once it is set, no batch is written.
	writeErr atomic.Pointer[error]

	// latest is the block before the next one the archive expects, 0 when it expects none yet.
	latest atomic.Uint64

	// mu guards the fields below, which ArchiveBlock shares with the periodic flush.
	mu  sync.Mutex
	cur *batch
	// next is the first block not yet archived, 0 when the archive is empty.
	next   uint64
	closed bool
}

var _ archive.ArchivalStore = (*Exporter)(nil)

// StartExporter creates the archive tables when they are missing and starts writing after the
// last block already archived.
func StartExporter(ctx context.Context, cfg clickhouse.Config) (*Exporter, error) {
	if err := migrate(ctx, cfg); err != nil {
		return nil, err
	}
	w, err := newWriter(cfg)
	if err != nil {
		return nil, err
	}
	next, err := w.nextBlock(ctx)
	if err != nil {
		w.close()
		return nil, err
	}
	e := &Exporter{
		writer: w,
		full:   make(chan *batch),
		free:   make(chan *batch, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		cur:    newBatch(),
		next:   next,
	}
	if next > 0 {
		e.latest.Store(next - 1)
	}
	e.free <- newBatch()
	go e.writeBatches()
	go e.flushPeriodically()
	logger.Info("archive exporter started", "addr", cfg.Addr, "database", cfg.Database, "next_block", next)
	return e, nil
}

// ArchiveBlock copies block into the current batch, keeping no reference to it. It returns an error
// for a block other than the one it expects next, for one it cannot encode, once a batch has failed to
// be written, and once the exporter is closed. When the block fills the batch, it waits for the
// previous batch to be written.
func (e *Exporter) ArchiveBlock(ctx context.Context, block *archive.ExecutedBlock) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	number := block.Number
	if e.closed {
		return fmt.Errorf("archive exporter is closed; block %d not archived", number)
	}
	if err := e.writeFailure(); err != nil {
		return err
	}
	if e.next == 0 {
		// Recorded before the block is accepted, so a restart that lost the first batch rolls the node
		// back to this block rather than starting the archive after it.
		if err := e.recordStart(ctx, number); err != nil {
			return err
		}
	}
	// Any other block would leave a gap or archive a block twice.
	if number != e.next {
		return fmt.Errorf("archive expects block %d, got block %d", e.next, number)
	}
	if err := e.cur.add(block); err != nil {
		return fmt.Errorf("archive block %d: %w", number, err)
	}
	e.next = number + 1
	if b := e.cur; b.blocks >= maxBatchBlocks || b.txs >= maxBatchTxs || b.bytes >= maxBatchBytes || b.maxRows() >= maxBatchRows {
		return e.flush(ctx)
	}
	return nil
}

// recordStart makes first the block an empty archive starts at, recorded as a committed row that ends at
// the block before it. The caller holds mu.
func (e *Exporter) recordStart(ctx context.Context, first uint64) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if err := e.writer.insertCommit(ctx, first, first-1, true); err != nil {
		return fmt.Errorf("record the archive's start at block %d: %w", first, err)
	}
	e.next = first
	e.latest.Store(first - 1)
	return nil
}

// LatestBlock returns the highest block the archive has committed, or for an archive that has committed
// none yet, the block before its start. It returns 0 when neither is known.
func (e *Exporter) LatestBlock() uint64 {
	return e.latest.Load()
}

// Close writes the blocks the exporter holds and stops it, returning the first failed write. It must
// be called after the last ArchiveBlock has returned.
func (e *Exporter) Close() error {
	e.closeOnce.Do(func() {
		close(e.stop)
		e.mu.Lock()
		e.closed = true
		_ = e.flush(context.Background())
		e.mu.Unlock()
		close(e.full)
		<-e.done
		e.writer.close()
	})
	return e.writeFailure()
}

// flush hands the current batch to the writer, waiting while it writes the previous one. The
// caller holds mu.
func (e *Exporter) flush(ctx context.Context) error {
	if e.cur.blocks == 0 {
		return nil
	}
	select {
	case e.full <- e.cur:
		e.cur = <-e.free
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// flushPeriodically hands the current batch to the writer every flushInterval, until Close. A
// writer still busy with the previous batch leaves the current one filling.
func (e *Exporter) flushPeriodically() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.mu.Lock()
			if !e.closed && e.cur.blocks > 0 {
				select {
				case e.full <- e.cur:
					e.cur = <-e.free
				default:
				}
			}
			e.mu.Unlock()
		case <-e.stop:
			return
		}
	}
}

// writeBatches writes the batches it receives on full and hands them back on free, until full is
// closed. After a write fails, it drops the rest.
func (e *Exporter) writeBatches() {
	defer close(e.done)
	for b := range e.full {
		if e.writeFailure() != nil {
			logger.Error("archive batch dropped after a failed write", "from", b.first, "to", b.last)
		} else if err := e.writeBatch(b); err != nil {
			logger.Error("archive batch write failed", "from", b.first, "to", b.last, "err", err)
			e.writeErr.Store(&err)
		}
		b.reset()
		e.free <- b
	}
}

// writeBatch writes b in a single attempt bounded by writeTimeout.
func (e *Exporter) writeBatch(b *batch) error {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	if err := e.writer.write(ctx, b); err != nil {
		return err
	}
	e.latest.Store(b.last)
	logger.Debug("archive batch committed", "from", b.first, "to", b.last, "txs", b.txs)
	return nil
}

// writeFailure returns the first failed write, or nil when every write so far has succeeded.
func (e *Exporter) writeFailure() error {
	if err := e.writeErr.Load(); err != nil {
		return *err
	}
	return nil
}

// A failed write is not retried: a retry inserts again the rows the failed attempt stored. Retrying
// needs every table to drop a repeated insert, with a non_replicated_deduplication_window in its
// settings and an insert_deduplication_token on every insert, and then writeBatches can call:
//
// // closingAttempts is how many times a batch is tried once the exporter is closing.
// const closingAttempts = 3
//
// // writeWithRetry writes b, retrying until it succeeds. Once the exporter is closing it gives up
// // after closingAttempts and reports false.
// func (e *Exporter) writeWithRetry(b *batch) bool {
// 	for attempt := 1; ; attempt++ {
// 		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
// 		err := e.writer.write(ctx, b)
// 		cancel()
// 		if err == nil {
// 			e.latest.Store(b.last)
// 			logger.Debug("archive batch committed", "from", b.first, "to", b.last, "txs", b.txs)
// 			return true
// 		}
// 		logger.Error("archive batch write failed; retrying", "from", b.first, "to", b.last, "attempt", attempt, "err", err)
// 		select {
// 		case <-time.After(backoff(attempt)):
// 		case <-e.stop:
// 			if attempt >= closingAttempts {
// 				logger.Error("archive exporter closing with an unwritten batch", "from", b.first, "to", b.last)
// 				return false
// 			}
// 		}
// 	}
// }
//
// func backoff(attempt int) time.Duration {
// 	return min(time.Duration(1<<min(attempt, 6))*100*time.Millisecond, 10*time.Second)
// }
