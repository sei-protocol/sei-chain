package statewal

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/seilog"
)

var _ StateWAL = (*stateWALImpl)(nil)

var logger = seilog.NewLogger("db", "state-db", "statewal")

// changesetMsg carries one Write's changesets to the serializer goroutine to be marshaled and accumulated
// into the current block's buffer.
type changesetMsg struct {
	blockNumber uint64
	cs          []*proto.NamedChangeSet
}

// endOfBlockMsg tells the serializer goroutine that the current block is complete: it appends the accumulated
// buffer to the underlying WAL as a single record and resets the buffer.
type endOfBlockMsg struct {
	blockNumber uint64
}

// flushMsg asks the serializer goroutine to flush the underlying WAL, signaling done when durable.
type flushMsg struct {
	done chan error
}

// rangeMsg asks the serializer goroutine to report the stored block range.
type rangeMsg struct {
	reply chan rangeReply
}

// The block range (and any error) reported by GetStoredRange.
type rangeReply struct {
	ok    bool
	start uint64
	end   uint64
	err   error
}

// pruneMsg asks the serializer goroutine to prune the underlying WAL below `through`.
type pruneMsg struct {
	through uint64
}

// iteratorMsg asks the serializer goroutine to create an iterator, so it is ordered after every prior write.
type iteratorMsg struct {
	startBlock uint64
	reply      chan iteratorReply
}

// The iterator (or an error) produced in response to an iteratorMsg.
type iteratorReply struct {
	iterator StateWALIterator
	err      error
}

// closeMsg asks the serializer goroutine to close the underlying WAL and shut down, signaling done when closed.
type closeMsg struct {
	done chan error
}

// A state WAL implemented as a thin, block-aware wrapper over a generic seiwal.WAL.
//
// The wrapper owns the block write-ordering contract (Write/SignalEndOfBlock) and the mapping of a block's
// changesets to a single opaque WAL record: the block number becomes the record index, and the block's
// changesets (accumulated across one or more Write calls) become the record payload. A single serializer
// goroutine marshals changesets off the caller's critical path — the throughput-sensitive path — and appends
// one record per block at end-of-block.
type stateWALImpl struct {
	// The configuration this WAL was opened with. Read-only after construction.
	config *Config

	// The underlying generic write-ahead log.
	wal seiwal.WAL

	// Caller entry points funnel through serializerChan as a single ordered stream to the serializer.
	serializerChan chan any

	// The hard-stop context the serializer watches. Cancelled by fail() on a fatal error and by Close() once
	// everything has drained.
	ctx    context.Context
	cancel context.CancelFunc

	// A child of ctx that the serializerChan producers watch, cancelled once the serializer stops reading so
	// an in-flight or future push aborts rather than deadlocking.
	senderCtx    context.Context
	senderCancel context.CancelFunc

	// Tracks the serializer goroutine so Close() can wait for it to exit.
	wg sync.WaitGroup

	// Guarantees the Close() shutdown sequence runs at most once.
	closeOnce sync.Once

	// Set by Close() so subsequent scheduling calls fail fast.
	closed atomic.Bool

	// The first unrecoverable background-goroutine error, surfaced to the caller by Close().
	asyncErr atomic.Pointer[error]

	// Guards the write-ordering contract state below, which is read/written synchronously in Write and
	// SignalEndOfBlock (not on the serializer goroutine).
	mu sync.Mutex
	// The block number of the most recent Write or SignalEndOfBlock.
	currentBlock uint64
	// Whether currentBlock has been finalized by SignalEndOfBlock.
	currentBlockEnded bool
	// Whether any block has been observed (this session or recovered from disk).
	hasCurrentBlock bool
}

// New opens (or creates) a state WAL in the configured directory, recovering any files left behind by a
// previous session.
func New(config *Config) (StateWAL, error) {
	return newStateWAL(config, nil)
}

// NewWithRollback opens a state WAL and deletes all data for blocks beyond rollbackBlockNumber before
// returning, so the WAL contains no block greater than rollbackBlockNumber.
func NewWithRollback(config *Config, rollbackBlockNumber uint64) (StateWAL, error) {
	return newStateWAL(config, &rollbackBlockNumber)
}

func newStateWAL(config *Config, rollbackThrough *uint64) (StateWAL, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid state WAL config: %w", err)
	}

	var wal seiwal.WAL
	var err error
	if rollbackThrough != nil {
		wal, err = seiwal.NewWithRollback(config.toSeiwalConfig(), *rollbackThrough)
	} else {
		wal, err = seiwal.New(config.toSeiwalConfig())
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open underlying WAL: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	senderCtx, senderCancel := context.WithCancel(ctx)

	w := &stateWALImpl{
		config:         config,
		wal:            wal,
		serializerChan: make(chan any, config.RequestBufferSize),
		ctx:            ctx,
		cancel:         cancel,
		senderCtx:      senderCtx,
		senderCancel:   senderCancel,
	}

	// Recover the write-ordering position from the highest block already on disk.
	ok, _, last, err := wal.Bounds()
	if err != nil {
		_ = wal.Close()
		return nil, fmt.Errorf("failed to read WAL bounds: %w", err)
	}
	if ok {
		w.currentBlock = last
		w.currentBlockEnded = true
		w.hasCurrentBlock = true
	}

	w.wg.Add(1)
	go w.serializerLoop()

	return w, nil
}

// Write schedules a set of changes for the given block number.
func (w *stateWALImpl) Write(blockNumber uint64, cs []*proto.NamedChangeSet) error {
	if w.closed.Load() {
		return fmt.Errorf("state WAL is closed")
	}
	if err := w.enforceWriteOrdering(blockNumber); err != nil {
		return fmt.Errorf("write rejected: %w", err)
	}
	if err := w.sendToSerializer(changesetMsg{blockNumber: blockNumber, cs: cs}); err != nil {
		return fmt.Errorf("failed to schedule write for block %d: %w", blockNumber, err)
	}
	return nil
}

// SignalEndOfBlock schedules the current block's accumulated changesets to be appended as a single record.
func (w *stateWALImpl) SignalEndOfBlock() error {
	if w.closed.Load() {
		return fmt.Errorf("state WAL is closed")
	}

	w.mu.Lock()
	if !w.hasCurrentBlock || w.currentBlockEnded {
		w.mu.Unlock()
		return fmt.Errorf("no block in progress to end")
	}
	blockNumber := w.currentBlock
	w.currentBlockEnded = true
	w.mu.Unlock()

	if err := w.sendToSerializer(endOfBlockMsg{blockNumber: blockNumber}); err != nil {
		return fmt.Errorf("failed to schedule end-of-block for block %d: %w", blockNumber, err)
	}
	return nil
}

// enforceWriteOrdering rejects a Write that violates the block-ordering rules (no decreasing block numbers; no
// advancing to a new block before the current one is ended) and records the new position when it is allowed.
func (w *stateWALImpl) enforceWriteOrdering(blockNumber uint64) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.hasCurrentBlock {
		w.currentBlock = blockNumber
		w.currentBlockEnded = false
		w.hasCurrentBlock = true
		return nil
	}
	if blockNumber < w.currentBlock {
		return fmt.Errorf("block number %d is less than the current block number %d", blockNumber, w.currentBlock)
	}
	if blockNumber == w.currentBlock {
		if w.currentBlockEnded {
			return fmt.Errorf("block number %d has already ended; cannot write more changes to it", blockNumber)
		}
		return nil
	}
	// blockNumber > currentBlock
	if !w.currentBlockEnded {
		return fmt.Errorf(
			"cannot write block %d before calling SignalEndOfBlock for block %d", blockNumber, w.currentBlock)
	}
	w.currentBlock = blockNumber
	w.currentBlockEnded = false
	return nil
}

// Flush blocks until all previously scheduled writes are durable.
func (w *stateWALImpl) Flush() error {
	done := make(chan error, 1)
	if err := w.sendToSerializer(flushMsg{done: done}); err != nil {
		return fmt.Errorf("failed to schedule flush: %w", err)
	}
	select {
	case err := <-done:
		return err // already wrapped by the underlying WAL, or nil on success
	case <-w.ctx.Done():
		if err := w.asyncError(); err != nil {
			return fmt.Errorf("flush aborted: %w", err)
		}
		return fmt.Errorf("flush aborted: %w", w.ctx.Err())
	}
}

// GetStoredRange reports the range of complete blocks stored in the WAL.
func (w *stateWALImpl) GetStoredRange() (bool, uint64, uint64, error) {
	reply := make(chan rangeReply, 1)
	if err := w.sendToSerializer(rangeMsg{reply: reply}); err != nil {
		return false, 0, 0, fmt.Errorf("failed to schedule stored-range query: %w", err)
	}
	select {
	case r := <-reply:
		if r.err != nil {
			return false, 0, 0, fmt.Errorf("stored-range query failed: %w", r.err)
		}
		return r.ok, r.start, r.end, nil
	case <-w.ctx.Done():
		if err := w.asyncError(); err != nil {
			return false, 0, 0, fmt.Errorf("stored-range query aborted: %w", err)
		}
		return false, 0, 0, fmt.Errorf("stored-range query aborted: %w", w.ctx.Err())
	}
}

// Prune schedules removal of whole underlying files below lowestBlockNumberToKeep. It does not block on
// completion.
func (w *stateWALImpl) Prune(lowestBlockNumberToKeep uint64) error {
	if err := w.sendToSerializer(pruneMsg{through: lowestBlockNumberToKeep}); err != nil {
		return fmt.Errorf("failed to schedule prune below block %d: %w", lowestBlockNumberToKeep, err)
	}
	return nil
}

// Iterator returns an iterator over the WAL starting at startingBlockNumber. Construction is ordered on the
// serializer goroutine after every prior write, so the iterator observes all previously scheduled writes.
func (w *stateWALImpl) Iterator(startingBlockNumber uint64) (StateWALIterator, error) {
	reply := make(chan iteratorReply, 1)
	if err := w.sendToSerializer(iteratorMsg{startBlock: startingBlockNumber, reply: reply}); err != nil {
		return nil, fmt.Errorf("failed to schedule iterator creation: %w", err)
	}
	select {
	case resp := <-reply:
		if resp.err != nil {
			return nil, fmt.Errorf("failed to create iterator: %w", resp.err)
		}
		return resp.iterator, nil
	case <-w.ctx.Done():
		if err := w.asyncError(); err != nil {
			return nil, fmt.Errorf("iterator creation aborted: %w", err)
		}
		return nil, fmt.Errorf("iterator creation aborted: %w", w.ctx.Err())
	}
}

// Close flushes pending writes, closes the underlying WAL, and releases resources.
func (w *stateWALImpl) Close() error {
	var closeErr error
	w.closeOnce.Do(func() {
		w.closed.Store(true)
		done := make(chan error, 1)
		if err := w.sendToSerializer(closeMsg{done: done}); err == nil {
			select {
			case closeErr = <-done:
			case <-w.ctx.Done():
			}
		}
		w.wg.Wait()
		w.cancel()
	})
	if err := w.asyncError(); err != nil {
		return fmt.Errorf("state WAL closed with error: %w", err)
	}
	return closeErr // already wrapped by the underlying WAL, or nil on a clean close
}

// sendToSerializer enqueues a message onto the serializer's input channel, aborting if the WAL is shutting
// down or has failed.
func (w *stateWALImpl) sendToSerializer(msg any) error {
	select {
	case w.serializerChan <- msg:
		return nil
	case <-w.senderCtx.Done():
		if err := w.asyncError(); err != nil {
			return fmt.Errorf("state WAL failed: %w", err)
		}
		return fmt.Errorf("state WAL is closed")
	}
}

// serializerLoop marshals each block's changesets into a per-block buffer and, at end-of-block, appends the
// buffer to the underlying WAL as a single record. Control messages (flush, range, prune, iterator, close) are
// handled in FIFO order relative to writes so they observe a consistent view. Runs on its own goroutine until
// close or a fatal error.
func (w *stateWALImpl) serializerLoop() {
	defer w.wg.Done()

	// The accumulated payload of the block currently being written, reused across blocks.
	var buf []byte

	for {
		var msg any
		select {
		case <-w.ctx.Done():
			return
		case msg = <-w.serializerChan:
		}

		switch m := msg.(type) {
		case changesetMsg:
			for _, ncs := range m.cs {
				var err error
				buf, err = appendChangeset(buf, ncs)
				if err != nil {
					w.fail(fmt.Errorf("failed to serialize changeset for block %d: %w", m.blockNumber, err))
					return
				}
			}
		case endOfBlockMsg:
			if err := w.wal.Append(m.blockNumber, buf); err != nil {
				w.fail(fmt.Errorf("failed to append block %d: %w", m.blockNumber, err))
				return
			}
			buf = buf[:0]
		case flushMsg:
			m.done <- w.wal.Flush()
		case rangeMsg:
			ok, first, last, err := w.wal.Bounds()
			m.reply <- rangeReply{ok: ok, start: first, end: last, err: err}
		case pruneMsg:
			if err := w.wal.Prune(m.through); err != nil {
				w.fail(fmt.Errorf("failed to prune below block %d: %w", m.through, err))
				return
			}
		case iteratorMsg:
			inner, err := w.wal.Iterator(m.startBlock)
			if err != nil {
				m.reply <- iteratorReply{err: err}
			} else {
				m.reply <- iteratorReply{iterator: newStateIterator(inner)}
			}
		case closeMsg:
			m.done <- w.wal.Close()
			// FIFO guarantees every prior write has been appended. Forbid further pushes so any
			// racing/future schedule aborts instead of deadlocking against the now-exiting serializer.
			w.senderCancel()
			return
		}
	}
}

// fail records the first fatal background error and triggers shutdown of the pipeline.
func (w *stateWALImpl) fail(err error) {
	w.asyncErr.CompareAndSwap(nil, &err)
	w.cancel()
	logger.Error("state WAL encountered a fatal error", "err", err)
}

// asyncError returns the first fatal background error, or nil if none occurred.
func (w *stateWALImpl) asyncError() error {
	if p := w.asyncErr.Load(); p != nil {
		return *p
	}
	return nil
}
