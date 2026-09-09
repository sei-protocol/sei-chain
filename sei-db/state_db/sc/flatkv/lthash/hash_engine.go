package lthash

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
)

/*

The HashEngine hashes using three pipelined phases:

-- Phase 1: Gather --

In order to compute a lattice hash, for each key-value pair that changed in a block, we must know
both the new value and the previous value. This phase is responsible for gathering these previous-new
pairs.

-- Phase 2: Hash --

For each changed key-value pair in a block, we hash both the previous value and the new value. This
phase fans out to a large work pool, since individual leaf hashes can be computed independently.

-- Phase 3: Combine --

In order to compute the final lattice hash, we need to "sum up" the individual leaf hashes. This operation
is done one block at a time, since block N's hash is a function of block N-1's hash.

*/

// Computes lattice hashes for flatKV. This utility has no recoverable errors.
type HashEngine struct {
	// gatherer reads each block's changed values and submits its leaf hashing to the pool.
	gatherer *blockGatherer

	// combiner sums each block's leaf hashes onto the block before it, and owns the running state.
	combiner *hashCombiner

	// ctx is cancelled when the engine is stopping, to release a caller waiting on work it will no
	// longer reach.
	ctx context.Context

	// cancel stops the gatherer and the combiner where they stand. Called by Close once both are
	// through, and by the store's own context.
	cancel context.CancelFunc

	// fatalErr latches the first failure. Nil until something fails.
	fatalErr atomic.Pointer[error]
}

// Construct a new hash engine.
func NewHashEngine(
	// Cancelling this stops the engine, abandoning the blocks it has not reached. Close stops it the
	// other way, hashing them first.
	parent context.Context,
	cfg *Config,
	// Used to compute the leaf hashes. Owned by the caller, and must stay open for at least as long as the
	// engine.
	pool threading.Pool,
	// The canonical set of database names so that we produce a hash for each DB for each block.
	dbNames []string,
	moduleParser ModuleParser,
	// The hash state the first scheduled block is measured against. A store with history passes what it
	// read off disk; one with none passes NewBlockHash(dbNames).
	seed *BlockHash,
) (*HashEngine, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate hash engine config: %w", err)
	}
	if pool == nil {
		return nil, fmt.Errorf("pool is nil")
	}
	if moduleParser == nil {
		return nil, fmt.Errorf("module parser is nil")
	}
	if seed == nil {
		return nil, fmt.Errorf("seed is nil")
	}

	ctx, cancel := context.WithCancel(parent)
	he := &HashEngine{ctx: ctx, cancel: cancel}
	he.gatherer = newBlockGatherer(
		cfg, newLeafHasher(pool, moduleParser, cfg.ChunkSize), ctx, cancel, he.brick)
	he.combiner = newHashCombiner(
		dbNames, seed, he.gatherer.combineJobChan, ctx, cfg.HashChanSize, he.brick)
	return he, nil
}

// Schedule a block to be hashed.
//
// The engine takes its own reservation on both views and releases it once it has read them. The
// caller keeps its own.
func (he *HashEngine) ScheduleHash(
	// This block's sealed view.
	current *sview.StoreView,
	// The preceding block's view, which is where each changed key's value before the block is read from.
	previous *sview.StoreView,
) error {
	if current == nil || previous == nil {
		return fmt.Errorf("schedule hash: current and previous views are both required")
	}
	request := &hashRequest{
		blockNumber: current.BlockHeight(),
		current:     current,
		previous:    previous,
	}
	if err := request.reserve(); err != nil {
		return fmt.Errorf("schedule hash for block %d: %w", request.blockNumber, err)
	}
	if err := he.enqueue(request); err != nil {
		return errors.Join(
			fmt.Errorf("schedule hash for block %d: %w", request.blockNumber, err),
			request.release())
	}
	return nil
}

// Returns a channel that returns block hashes, as they are computed.
func (he *HashEngine) AwaitHash() <-chan *BlockHash {
	return he.combiner.blockHashChan
}

// Flush blocks until the engine has published a hash for every block scheduled so far.
func (he *HashEngine) Flush() error {
	request := newFlushRequest()
	if err := he.enqueue(request); err != nil {
		return fmt.Errorf("flush hash engine: %w", err)
	}
	select {
	case <-request.doneChan:
	case <-he.ctx.Done():
		// A stopping engine never reaches this request. The blocks behind it are abandoned rather than
		// hashed, and their rows are still in the WAL for replay to recover.
		if err := he.errorIfBricked(); err != nil {
			return fmt.Errorf("flush hash engine: %w", err)
		}
		return fmt.Errorf("flush hash engine: engine is stopping: %w", he.ctx.Err())
	}
	if err := he.errorIfBricked(); err != nil {
		return fmt.Errorf("flush hash engine: %w", err)
	}
	return nil
}

// Close stops the engine once it has hashed every block scheduled so far, publishing each one, and
// reports the latched error if it failed.
//
// Never call concurrently with another method: behaviour is undefined if anything else is in flight.
// Cancelling the engine's context stops it the other way, abandoning whatever it had not reached.
func (he *HashEngine) Close() error {
	// The request travels the same queue as the blocks, which is what makes every block scheduled before
	// this call reach the combiner first. An engine already stopping refuses it, and there is nothing to
	// drain in that case because the abandonment is already under way.
	_ = he.enqueue(newCloseRequest())

	he.gatherer.wg.Wait()
	he.combiner.wg.Wait()

	// Cancelled only once both phases are through. The combiner gives up on a publish under a cancelled
	// context, so cancelling any earlier would abandon the very blocks this call is draining.
	he.cancel()

	if err := he.errorIfBricked(); err != nil {
		return fmt.Errorf("close hash engine: %w", err)
	}
	return nil
}

// enqueue puts a message on the gatherer's queue, blocking while it is full. Cleaning up after a message
// it could not deliver belongs to the caller, which is the only one that knows whether the message owns
// anything.
func (he *HashEngine) enqueue(message any) error {
	if err := he.errorIfBricked(); err != nil {
		return fmt.Errorf("hash engine failed: %w", err)
	}
	select {
	case he.gatherer.scheduledBlockChan <- message:
		return nil
	case <-he.ctx.Done():
		return fmt.Errorf("hash engine is stopping: %w", he.ctx.Err())
	}
}

// brick latches err as the engine's fatal error and stops it.
func (he *HashEngine) brick(err error) {
	he.fatalErr.CompareAndSwap(nil, &err)
}

// errorIfBricked reports the latched error, or nil if the engine has not failed.
func (he *HashEngine) errorIfBricked() error {
	if err := he.fatalErr.Load(); err != nil {
		return *err
	}
	return nil
}
