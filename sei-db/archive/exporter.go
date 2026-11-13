package archive

import (
	"context"
	"sync"
	"time"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

var logger = seilog.NewLogger("db", "archive")

const (
	// queueBlocks is how many executed blocks Export buffers before it blocks.
	queueBlocks = 16
	// A batch is written once it holds maxBatchBlocks blocks, maxBatchTxs transactions,
	// maxBatchRows rows in one table or maxBatchBytes of variable-length data, or once its first
	// block has waited flushInterval.
	maxBatchBlocks = 500
	maxBatchTxs    = 500_000
	maxBatchRows   = 1_000_000
	maxBatchBytes  = 256 << 20
	flushInterval  = time.Second
	// closingAttempts is how many times a batch is tried once the exporter is closing.
	closingAttempts = 3
)

// Exporter writes every block the executor produces to the archive, in order and in batches, in
// the background.
type Exporter struct {
	writer    *writer
	queue     chan executedBlock
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

var _ evmonly.ResultSink = (*Exporter)(nil)

// executedBlock is a block waiting in the export queue, with its result and the func that
// releases them.
type executedBlock struct {
	block   evmonly.PreparedBlock
	result  *evmonly.BlockResult
	release func()
}

// StartExporter creates the archive tables when they are missing and starts writing after the
// last block already archived.
func StartExporter(ctx context.Context, cfg Config) (*Exporter, error) {
	if err := migrate(ctx, cfg); err != nil {
		return nil, err
	}
	w := newWriter(cfg)
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
		queue:  make(chan executedBlock, queueBlocks),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go e.run(next)
	logger.Info("archive exporter started", "addr", cfg.Addr, "database", cfg.Database, "next_block", next)
	return e, nil
}

// StoreBlockResult queues an executed block for the archive, which calls release once it has
// copied the block's rows. It blocks while the queue is full, so a stalled archive slows the
// executor rather than losing blocks. A closed exporter releases and drops the block. On error
// release is not called.
func (e *Exporter) StoreBlockResult(ctx context.Context, block evmonly.PreparedBlock, result *evmonly.BlockResult, release func()) error {
	select {
	case <-e.stop:
		release()
		logger.Error("archive exporter is closed; block not archived", "block", block.Context.Number)
		return nil
	default:
	}
	select {
	case e.queue <- executedBlock{block: block, result: result, release: release}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close writes the blocks already queued and stops the exporter. It must be called after the
// last StoreBlockResult has returned.
func (e *Exporter) Close() {
	e.closeOnce.Do(func() {
		close(e.stop)
		<-e.done
		e.writer.close()
	})
}

// run adds queued blocks to a batch while the previous batch is written, until Close. next is the
// first block not yet archived, 0 when the archive is empty.
func (e *Exporter) run(next uint64) {
	defer close(e.done)
	// Two batches take turns: one fills while the other is written.
	free := make(chan *batch, 2)
	free <- newBatch()
	free <- newBatch()
	full := make(chan *batch)
	written := make(chan struct{})
	go e.writeBatches(full, free, written)

	cur := <-free
	var deadline <-chan time.Time
	flush := func() {
		if cur.blocks == 0 {
			return
		}
		full <- cur
		cur = <-free
		deadline = nil
	}
	add := func(b *executedBlock) {
		defer b.release()
		number := b.block.Context.Number
		if next != 0 && number < next {
			return // already archived
		}
		if next != 0 && number > next {
			logger.Error("archive gap: blocks are missing and must be backfilled", "from", next, "to", number-1)
			flush() // a batch holds consecutive blocks only
		}
		next = number + 1
		if err := cur.add(&b.block, b.result); err != nil {
			logger.Error("cannot archive block; it must be backfilled", "block", number, "err", err)
			flush()
			return
		}
		if deadline == nil {
			deadline = time.After(flushInterval)
		}
		if cur.blocks >= maxBatchBlocks || cur.txs >= maxBatchTxs || cur.bytes >= maxBatchBytes || cur.maxRows() >= maxBatchRows {
			flush()
		}
	}
	for {
		select {
		case b := <-e.queue:
			add(&b)
		case <-deadline:
			flush()
		case <-e.stop:
			for {
				select {
				case b := <-e.queue:
					add(&b)
				default:
					flush()
					close(full)
					<-written
					return
				}
			}
		}
	}
}

// writeBatches writes the batches it receives on full and hands them back on free, until full is
// closed. After a batch is given up on while closing, it drops the rest.
func (e *Exporter) writeBatches(full <-chan *batch, free chan<- *batch, written chan<- struct{}) {
	defer close(written)
	failed := false
	for b := range full {
		if failed {
			logger.Error("archive exporter closing; batch not archived", "from", b.first, "to", b.last)
		} else {
			failed = !e.writeWithRetry(b)
		}
		b.reset()
		free <- b
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
