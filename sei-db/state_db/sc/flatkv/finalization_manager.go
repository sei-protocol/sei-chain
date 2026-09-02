package flatkv

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// FinalizationManager records each block's lattice hashes onto that block's own views, in the same
// atomic batch as the data they describe, off the execution goroutine.
//
// Sealed blocks go in through Offer(), which reserves the view and releases it once the block's
// metadata has been written, and hashes come out of HashChan(), one per block in block
// order and only once that write has happened. PublishedHash() answers with the most recent.
//
// There are no recoverable errors. The first failure is latched and stops the manager, and every later
// call reports it.
type FinalizationManager struct {
	// hashes is the engine's stream. This manager is its sole consumer, and must drain it to completion
	// even while failing, or the engine blocks forever trying to publish.
	engineHashChan <-chan *lthash.BlockHash

	// queue carries sealed blocks and control messages, in block order.
	messageChan chan any

	// published is the outbound stream, one entry per block, put there only once the block's metadata is
	// on its way to disk.
	publishedHashChan chan *lthash.BlockHash

	// latest is the most recently finalized block's hash, for a reader that wants the current answer
	// rather than the stream. Single writer, so a plain atomic swap is enough.
	latest atomic.Pointer[lthash.BlockHash]

	// ctx is cancelled when the manager is stopping, to release a publish that nobody is reading.
	ctx context.Context

	// cancel stops the goroutine. Called by Close, and by the store's own context.
	cancel context.CancelFunc

	// streamClosed guards publishedHashChan, which is closed either when a block fails or at teardown,
	// whichever comes first.
	streamClosed sync.Once

	// wg tracks the goroutine, so that Close can wait for it to return.
	wg sync.WaitGroup

	// fatalErr latches the first failure. Nil until something fails.
	fatalErr atomic.Pointer[error]

	// hashLogger receives each block's hashes as it is finalized. Never nil.
	hashLogger hashlog.HashLogger

	// reportingFailed stops reporting after the logger first rejects a hash, so a logger closed
	// underneath this manager costs one log line rather than one per block.
	reportingFailed bool
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
	// Depth of the channel finalized hashes are published on.
	chanSize uint32,
	// Receives each block's hashes as it is finalized.
	hl hashlog.HashLogger,
) *FinalizationManager {
	ctx, cancel := context.WithCancel(parent)
	fm := &FinalizationManager{
		engineHashChan:    engineHashChan,
		messageChan:       make(chan any, max(queueSize, 1)),
		publishedHashChan: make(chan *lthash.BlockHash, max(chanSize, 1)),
		ctx:               ctx,
		cancel:            cancel,
		hashLogger:        hl,
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

// HashChan returns the stream of block hashes, one per block in block order.
//
// A block that failed arrives with Error set and the stream closes behind it, since nothing is
// published after one. It also closes when the manager does.
func (fm *FinalizationManager) HashChan() <-chan *lthash.BlockHash {
	return fm.publishedHashChan
}

// Flush blocks until the manager has finalized every block offered so far.
func (fm *FinalizationManager) Flush() error {
	request := newFinalizationFlushRequest()
	if err := fm.enqueue(request); err != nil {
		return fmt.Errorf("flush finalization manager: %w", err)
	}
	<-request.doneChan
	if err := fm.errorIfBricked(); err != nil {
		return fmt.Errorf("flush finalization manager: %w", err)
	}
	return nil
}

// Close stops the manager and waits for it to finish, reporting the latched error if it failed.
//
// Never call concurrently with another method: behaviour is undefined if anything else is in flight.
// Blocks that have been offered but not yet finalized are abandoned rather than
// finished — their reservations are released, and their rows are still in the WAL for replay to
// recover.
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

// enqueue puts a message on the queue, blocking while it is full.
func (fm *FinalizationManager) enqueue(message any) error {
	if err := fm.errorIfBricked(); err != nil {
		return fmt.Errorf("finalization manager failed: %w", err)
	}
	fm.messageChan <- message
	return nil
}

// run finalizes blocks until the manager is stopped or a block fails.
func (fm *FinalizationManager) run() {
	defer fm.wg.Done()
	defer fm.closeStream()

	failed := false
	for {
		select {
		case message := <-fm.messageChan:
			if failed {
				// Once a block has failed, the hashes any later block would record cannot be
				// trusted, so nothing more is written. What is still queued is given back rather
				// than finalized.
				fm.abandonMessage(message)
				continue
			}
			if failed = !fm.handle(message); failed {
				// The stream is closed on failure rather than left to teardown, because nothing is
				// published after a failed block: a consumer waiting on the next hash would otherwise
				// wait until the store closed.
				fm.closeStream()
			}
		case <-fm.ctx.Done():
			fm.abandon()
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
			// Published before the failure is latched, because a consumer reading the stream has to be
			// told the block failed; a closed channel alone reads as an orderly end.
			fm.publish(&lthash.BlockHash{BlockNumber: request.blockNumber, Error: err})
			fm.brick(err)
			return false
		}
		return !stopped
	case *finalizationFlushRequest:
		close(request.doneChan)
		return true
	default:
		fm.brick(fmt.Errorf("unknown finalization message type %T", message))
		return false
	}
}

// finalize writes one block's hashes onto its own views, releases its reservation, and publishes the
// hash.
// It reports stopped when the engine has no more hashes to give, which is teardown rather than failure.
func (fm *FinalizationManager) finalize(pending *pendingFinalization) (stopped bool, err error) {
	hash, ok := <-fm.engineHashChan
	if !ok {
		// The engine has stopped, so this block will never be hashed. That is teardown rather than
		// failure: its rows are in the WAL and replay recovers them. Discarding releases the reservation,
		// which is the part that must not be skipped.
		return true, fm.discard(pending)
	}
	if hash.Error != nil {
		return false, errors.Join(
			fmt.Errorf("hash block %d: %w", pending.blockNumber, hash.Error),
			fm.discard(pending))
	}
	if hash.BlockNumber != pending.blockNumber {
		return false, errors.Join(
			fmt.Errorf("finalization is out of step: holding block %d, hashed block %d",
				pending.blockNumber, hash.BlockNumber),
			fm.discard(pending))
	}

	for _, dbView := range pending.blockView.Views() {
		if err := finalizeStore(dbView, pending.blockNumber, pending.alreadyHave, hash); err != nil {
			return false, errors.Join(
				fmt.Errorf("finalize %s at block %d: %w", dbView.Name(), pending.blockNumber, err),
				pending.release())
		}
	}

	// The reservation is only needed while the writes above happen. Released here rather than after
	// publishing so the databases resume flushing even if nothing is reading the stream.
	if err := pending.release(); err != nil {
		return false, fmt.Errorf("release block %d after finalizing: %w", pending.blockNumber, err)
	}

	fm.latest.Store(hash)
	fm.reportHashes(hash)
	fm.publish(hash)
	return false, nil
}

// discard finalizes a block's views with nothing recorded and releases its reservation, for a block
// that will never get a hash. Releasing the last reservation on an unfinalized view is a fatal error in the view
// manager, so an abandoned block still has to be finalized — and its data is still in the WAL, so a
// restart recovers it.
func (fm *FinalizationManager) discard(pending *pendingFinalization) error {
	var errs []error
	for _, dbView := range pending.blockView.Views() {
		if err := dbView.Finalize(nil); err != nil {
			errs = append(errs, fmt.Errorf("finalize discarded %s: %w", dbView.Name(), err))
		}
	}
	errs = append(errs, pending.release())
	return errors.Join(errs...)
}

// abandon gives back everything still queued, without finalizing it. Queued blocks are discarded rather
// than finalized — after a failure the hashes they would record cannot be trusted, and during teardown
// they have no hashes at all — but their reservations are released either way, since a view left
// reserved can never flush. The engine's stream is drained so it is not left blocked publishing into it.
func (fm *FinalizationManager) abandon() {
	for {
		select {
		case message := <-fm.messageChan:
			fm.abandonMessage(message)
		default:
			fm.drainHashes()
			return
		}
	}
}

// abandonMessage gives one message back without acting on it: a block is discarded, which releases its
// reservation, and anything with a waiting caller is answered so that caller is not left blocked.
func (fm *FinalizationManager) abandonMessage(message any) {
	switch request := message.(type) {
	case *pendingFinalization:
		if err := fm.discard(request); err != nil {
			logger.Error("failed to discard an abandoned block",
				"version", request.blockNumber, "err", err)
		}
	case *finalizationFlushRequest:
		close(request.doneChan)
	default:
		fm.brick(fmt.Errorf("unknown finalization message type %T", message))
	}
}

// drainHashes reads the engine's stream to completion.
//
// The engine blocks publishing a hash nobody reads, and this manager is its only reader, so a manager
// that stopped reading would leave the engine's own Close unable to return.
func (fm *FinalizationManager) drainHashes() {
	for range fm.engineHashChan { //nolint:revive // draining is the point; the values are already accounted for
	}
}

// publish puts a block's hash on the outbound stream, giving up if the manager is stopping.
//
// Blocking here is the backpressure that stops a consumer falling arbitrarily far behind. Giving up on
// shutdown costs nothing: the block's metadata is already written by this point, so the hash is a
// notification rather than a durability step, and a stopped manager has no reader left to notify.
func (fm *FinalizationManager) publish(hash *lthash.BlockHash) {
	select {
	case fm.publishedHashChan <- hash:
	case <-fm.ctx.Done():
	}
}

// closeStream closes the outbound stream, which happens exactly once however often it is called.
func (fm *FinalizationManager) closeStream() {
	fm.streamClosed.Do(func() { close(fm.publishedHashChan) })
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
