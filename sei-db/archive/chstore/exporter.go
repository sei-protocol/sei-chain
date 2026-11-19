package chstore

import (
	"context"
	"errors"
	"sync"
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
	maxBatchBlocks = 500
	maxBatchTxs    = 500_000
	maxBatchRows   = 1_000_000
	maxBatchBytes  = 256 << 20
	flushInterval  = time.Second
	// closingAttempts is how many times a batch is tried once the exporter is closing.
	closingAttempts = 3
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

	// lost reports that a batch was given up on while closing. writeBatches sets it before done
	// closes.
	lost bool

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
	last, ok, err := w.lastCommitted(ctx)
	if err != nil {
		w.close()
		return nil, err
	}
	var next uint64
	if ok {
		next = last + 1
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
	e.free <- newBatch()
	go e.writeBatches()
	go e.flushPeriodically()
	logger.Info("archive exporter started", "addr", cfg.Addr, "database", cfg.Database, "next_block", next)
	return e, nil
}

// ArchiveBlock copies block into the current batch, keeping no reference to it. When that fills the
// batch it waits for the previous batch to be written, so a stalled archive slows the executor
// rather than losing blocks. A closed exporter drops the block.
func (e *Exporter) ArchiveBlock(ctx context.Context, block *archive.ExecutedBlock) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	number := block.Number
	if e.closed {
		logger.Error("archive exporter is closed; block not archived", "block", number)
		return nil
	}
	if e.next != 0 && number < e.next {
		return nil // already archived
	}
	if e.next != 0 && number > e.next {
		logger.Error("archive gap: blocks are missing and must be backfilled", "from", e.next, "to", number-1)
		// A batch holds consecutive blocks only.
		if err := e.flush(ctx); err != nil {
			return err
		}
	}
	e.next = number + 1
	if err := e.cur.add(block); err != nil {
		logger.Error("cannot archive block; it must be backfilled", "block", number, "err", err)
		return e.flush(ctx)
	}
	if b := e.cur; b.blocks >= maxBatchBlocks || b.txs >= maxBatchTxs || b.bytes >= maxBatchBytes || b.maxRows() >= maxBatchRows {
		return e.flush(ctx)
	}
	return nil
}

// Close writes the blocks the exporter holds and stops it, reporting an error when some of them could
// not be written. It must be called after the last ArchiveBlock has returned.
func (e *Exporter) Close() error {
	e.closeOnce.Do(func() {
		// Before the last flush, so a batch the writer is still retrying is given up on rather than
		// waited for.
		close(e.stop)
		e.mu.Lock()
		e.closed = true
		_ = e.flush(context.Background())
		e.mu.Unlock()
		close(e.full)
		<-e.done
		e.writer.close()
	})
	if e.lost {
		return errors.New("archive exporter closed with unwritten blocks")
	}
	return nil
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
// closed. After a batch is given up on while closing, it drops the rest.
func (e *Exporter) writeBatches() {
	defer close(e.done)
	for b := range e.full {
		if e.lost {
			logger.Error("archive exporter closing; batch not archived", "from", b.first, "to", b.last)
		} else {
			e.lost = !e.writeWithRetry(b)
		}
		b.reset()
		e.free <- b
	}
}

// writeWithRetry writes b, retrying until it succeeds. Once the exporter is closing it gives up
// after closingAttempts and reports false.
func (e *Exporter) writeWithRetry(b *batch) bool {
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		err := e.writer.write(ctx, b)
		cancel()
		if err == nil {
			logger.Debug("archive batch committed", "from", b.first, "to", b.last, "txs", b.txs)
			return true
		}
		logger.Error("archive batch write failed; retrying", "from", b.first, "to", b.last, "attempt", attempt, "err", err)
		select {
		case <-time.After(backoff(attempt)):
		case <-e.stop:
			if attempt >= closingAttempts {
				logger.Error("archive exporter closing with an unwritten batch", "from", b.first, "to", b.last)
				return false
			}
		}
	}
}

func backoff(attempt int) time.Duration {
	return min(time.Duration(1<<min(attempt, 6))*100*time.Millisecond, 10*time.Second)
}
