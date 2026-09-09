package flatkv

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
)

// FinalizationManager records each block's lattice hashes onto that block's own views, in the same
// atomic batch as the data they describe, off the execution goroutine.
//
// Sealed blocks go in through Offer(), which reserves the view and releases it once the block's
// metadata has been written, and hashes go out to the store's registered listeners, one per block in
// block order and only once that write has happened. PublishedHash() answers with the most recent.
//
// There are no recoverable errors. The first failure is latched and stops the manager, and every later
// call reports it.
type FinalizationManager struct {
	// engineHashChan is the engine's stream, one hash per block scheduled, in block order. This manager
	// is its sole consumer.
	engineHashChan <-chan *lthash.BlockHash

	// queue carries sealed blocks and control messages, in block order.
	messageChan chan any

	// latest is the most recently finalized block's hash, for a reader that wants the current answer
	// rather than a delivery. Single writer, so a plain atomic swap is enough.
	latest atomic.Pointer[lthash.BlockHash]

	// ctx is cancelled when the manager is stopping, and is handed to each listener so that one
	// blocking on a block's hash is released by teardown.
	ctx context.Context

	// cancel stops the goroutine. Called by Close, and by the store's own context.
	cancel context.CancelFunc

	// wg tracks the goroutine, so that Close can wait for it to return.
	wg sync.WaitGroup

	// fatalErr latches the first failure. Nil until something fails.
	fatalErr atomic.Pointer[error]

	// listeners receives each block's hash once it has been finalized. Owned by the store, so it
	// outlives this manager. Never nil.
	listeners *hashListenerRegistry
}

// newFinalizationManager starts a manager consuming the hash engine's stream.
func newFinalizationManager(
	// Cancelling this stops the manager, exactly as Close does.
	parent context.Context,
	// The engine's output. This manager is its only reader.
	engineHashChan <-chan *lthash.BlockHash,
	// The hash of the height the store loaded at, so that a reader has an answer before the first block
	// is finalized.
	loaded *lthash.BlockHash,
	// How many offered blocks may wait to be finalized before Offer blocks.
	queueSize uint32,
	// Receives each block's hash once it has been finalized.
	listeners *hashListenerRegistry,
) *FinalizationManager {
	ctx, cancel := context.WithCancel(parent)
	fm := &FinalizationManager{
		engineHashChan: engineHashChan,
		messageChan:    make(chan any, max(queueSize, 1)),
		ctx:            ctx,
		cancel:         cancel,
		listeners:      listeners,
	}
	fm.latest.Store(loaded)
	fm.wg.Add(1)
	go fm.run()
	return fm
}

// Offer hands a sealed block to the manager, to be finalized once its hash arrives.
//
// The manager takes its own reservation on the view and releases it once the block's metadata has
// been written. The caller keeps its own.
//
// Blocks while the manager is too far behind.
func (fm *FinalizationManager) Offer(
	blockNumber int64,
	// The block's sealed view, which this block's hashes are recorded onto.
	blockView *sview.StoreView,
	// The replay skip list: the height each database had already reached when replay started, or nil
	// outside replay.
	alreadyHave map[string]int64,
) error {
	if err := blockView.Reserve(); err != nil {
		return fmt.Errorf("reserve block %d for finalization: %w", blockNumber, err)
	}
	pending := &pendingFinalization{
		blockNumber: blockNumber,
		blockView:   blockView,
		alreadyHave: alreadyHave,
	}
	if err := fm.enqueue(pending); err != nil {
		return errors.Join(
			fmt.Errorf("offer block %d for finalization: %w", blockNumber, err),
			pending.release())
	}
	return nil
}

// PublishedHash returns the most recently finalized block's hash. It is the height the store loaded at
// until the first block has been finalized, and lags the committed version by however far this manager
// is behind.
func (fm *FinalizationManager) PublishedHash() *lthash.BlockHash {
	return fm.latest.Load()
}

// Flush blocks until the manager has finalized every block offered so far and dispatched each of
// their hashes to every registered listener.
func (fm *FinalizationManager) Flush() error {
	request := newFinalizationFlushRequest()
	if err := fm.enqueue(request); err != nil {
		return fmt.Errorf("flush finalization manager: %w", err)
	}
	select {
	case <-request.doneChan:
	case <-fm.ctx.Done():
		// A stopping manager never reaches this request. The blocks behind it are abandoned rather than
		// finalized, which Close reports, and their rows are still in the WAL for replay to recover.
	}
	if err := fm.errorIfBricked(); err != nil {
		return fmt.Errorf("flush finalization manager: %w", err)
	}
	return nil
}

// Close stops the manager and waits for it to finish, reporting the latched error if it failed.
//
// Never call concurrently with another method: behaviour is undefined if anything else is in flight.
// Blocks that have been offered but not yet finalized are abandoned rather than finished; the WAL
// still holds them for replay to recover.
//
// The hash engine must be closed before this, so that this manager's read of its stream terminates.
func (fm *FinalizationManager) Close() error {
	fm.cancel()
	fm.wg.Wait()
	if err := fm.errorIfBricked(); err != nil {
		return fmt.Errorf("close finalization manager: %w", err)
	}
	return nil
}

// enqueue puts a message on the queue, blocking while it is full and failing once the manager stops.
func (fm *FinalizationManager) enqueue(message any) error {
	if err := fm.errorIfBricked(); err != nil {
		return fmt.Errorf("finalization manager failed: %w", err)
	}
	select {
	case fm.messageChan <- message:
		return nil
	case <-fm.ctx.Done():
		return fmt.Errorf("finalization manager is stopping: %w", fm.ctx.Err())
	}
}

// run finalizes blocks until the manager is stopped or a block fails. It cancels the manager's context
// on the way out, whatever the reason: everything waiting on this manager waits under that context, and
// this goroutine is the only thing that can release it.
func (fm *FinalizationManager) run() {
	defer fm.wg.Done()
	defer fm.cancel()

	for {
		select {
		case message := <-fm.messageChan:
			if !fm.handle(message) {
				// Whatever is still queued is left as it was offered. An unfinalized view never
				// flushes, so those blocks' rows stay out of the databases and each one's recorded
				// version keeps matching what it holds.
				return
			}
		case <-fm.ctx.Done():
			return
		}
	}
}

// handle deals with one message, reporting whether the manager may continue.
func (fm *FinalizationManager) handle(message any) bool {
	switch request := message.(type) {
	case *pendingFinalization:
		stopped, err := fm.finalize(request)
		if err != nil {
			fm.brick(err)
			return false
		}
		return !stopped
	case *finalizationFlushRequest:
		// Answering here is what makes a flush mean the listeners have the hashes: every block queued
		// ahead of this request has already been dispatched, on this goroutine, before it is reached.
		close(request.doneChan)
		return true
	default:
		fm.brick(fmt.Errorf("unknown finalization message type %T", message))
		return false
	}
}

// finalize writes one block's hashes onto its own views, releases its reservation, and hands the hash
// to the listeners.
// It reports stopped when the engine has no more hashes to give, which is teardown rather than failure.
func (fm *FinalizationManager) finalize(pending *pendingFinalization) (stopped bool, err error) {
	hash, ok := <-fm.engineHashChan
	if !ok {
		// The engine has stopped, so this block will never be hashed. That is teardown rather than
		// failure: the block is abandoned where it is, and replay recovers it from the WAL.
		return true, nil
	}
	if hash.Error != nil {
		return false, fmt.Errorf("hash block %d: %w", pending.blockNumber, hash.Error)
	}
	if hash.BlockNumber != pending.blockNumber {
		return false, fmt.Errorf("finalization is out of step: holding block %d, hashed block %d",
			pending.blockNumber, hash.BlockNumber)
	}

	for _, dbView := range pending.blockView.Views() {
		if err := finalizeStore(dbView, pending.blockNumber, pending.alreadyHave, hash); err != nil {
			return false, fmt.Errorf("finalize %s at block %d: %w", dbView.Name(), pending.blockNumber, err)
		}
	}

	// The reservation is only needed while the writes above happen. Released before the hash goes out
	// so the databases resume flushing even while a listener is still working.
	if err := pending.release(); err != nil {
		return false, fmt.Errorf("release block %d after finalizing: %w", pending.blockNumber, err)
	}

	fm.latest.Store(hash)
	return false, fm.listeners.dispatch(fm.ctx, hash)
}

// brick latches err as the manager's fatal error and stops it.
func (fm *FinalizationManager) brick(err error) {
	fm.fatalErr.CompareAndSwap(nil, &err)
}

// errorIfBricked reports the latched error, or nil if the manager has not failed.
func (fm *FinalizationManager) errorIfBricked() error {
	if err := fm.fatalErr.Load(); err != nil {
		return *err
	}
	return nil
}
