package statewal

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

var _ StateWAL = (*stateWALImpl)(nil)

// A WAL for storing state changesets by block number.
//
// Not safe for concurrent use; see the StateWAL interface doc. Two surfaces are exceptions. The
// gc.PrunableStore surface in state_wal_gc.go runs on the collector's goroutine, and touches only
// lastBlock and the WAL underneath. The BUD goroutine in state_wal_bud.go touches only ctx, cancel,
// budChan, budListeners, and wg.
type stateWALImpl struct {
	// The underlying generic WAL, keyed by block number, whose payload is a block's changesets.
	wal seiwal.WAL[[]*proto.NamedChangeSet]

	// Closed by Close() so subsequent calls fail fast.
	closed utils.CloseMarker[stateWALImpl]

	// Cancelled when the WAL stops: by fail() with the fatal error that bricked the WAL as its cause, or by
	// Close() with a nil cause. Every blocking operation aborts on it, and it is the ctx passed to BUD listeners.
	ctx context.Context

	// Cancels ctx, recording the fatal error (or nil) as its cause. Only the first call takes effect.
	cancel context.CancelCauseFunc

	// Carries written blocks and flush requests from the caller to the BUD goroutine, in order.
	budChan chan any

	// The listeners each block's BUD is delivered to.
	budListeners *budListenerRegistry

	// Tracks the BUD goroutine so Close() can wait for it to exit.
	wg sync.WaitGroup

	// The highest block number written. Atomic because the garbage collector reads it off-goroutine
	// (GetLatestBlock); Write is its only mutator.
	//
	// 0 also means nothing has been written yet, so a WAL whose only block is block 0 is
	// indistinguishable from an empty one.
	lastBlock atomic.Uint64

	// Whether any block has been written (this session or recovered from disk), which is what tells an
	// empty WAL apart from one holding only block 0. Only ever touched by the single caller.
	hasBlock bool
}

// New opens (or creates) a state WAL in the configured directory, recovering any files left behind by a
// previous session.
func New(config *Config) (StateWAL, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid state WAL config: %w", err)
	}
	wal, err := seiwal.NewGenericWAL[[]*proto.NamedChangeSet](
		config.toSeiwalConfig(), serializeChangesets, deserializeChangesets)
	if err != nil {
		return nil, fmt.Errorf("failed to open state WAL: %w", err)
	}
	return newStateWAL(wal, config.BUDBufferSize)
}

// GetRange reports the range of block numbers stored in the state WAL directory configured by config,
// without constructing a live StateWAL. Like the seiwal function it wraps, it runs the recovery/sanity pass
// (which seals any unsealed file left by a prior session) before reading, so it mutates the directory.
//
// The range is read from sealed file names only, so its cost is a directory listing regardless of how much
// data the WAL holds; content is not checked. Use VerifyIntegrity to check for corruption.
//
// NOT SAFE FOR CONCURRENT USE with a live StateWAL, or with another GetRange/PruneAfter, on the same
// directory: it seals files a running WAL owns. Call it only while no StateWAL is open there (e.g. offline,
// at startup before New). For a range query against a live WAL use the instance method GetStoredRange
// instead.
func GetRange(config *Config) (bool, uint64, uint64, error) {
	ok, first, last, err := seiwal.GetRange(config.Path)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to read state WAL range: %w", err)
	}
	return ok, first, last, nil
}

// PruneAfter deletes all data for blocks after highestBlockToKeep from the state WAL directory configured by
// config, without constructing a live StateWAL. It runs the recovery/sanity pass, applies the rollback, and
// re-scans the result structurally (file names / sequence contiguity, not contents); blocks with a number
// <= highestBlockToKeep are kept.
//
// NOT SAFE FOR CONCURRENT USE with a live StateWAL, or with another GetRange/PruneAfter, on the same
// directory: it seals, rewrites, and removes files a running WAL owns. Call it only while no StateWAL is open
// there (e.g. offline, at startup before New).
func PruneAfter(config *Config, highestBlockToKeep uint64) error {
	if err := seiwal.PruneAfter(config.Path, highestBlockToKeep); err != nil {
		return fmt.Errorf("failed to prune state WAL: %w", err)
	}
	return nil
}

// VerifyIntegrity reads every sealed file in the state WAL directory configured by config and confirms each
// record's CRC and each file's name-versus-content range. This is the expensive O(total stored bytes) check
// that New/GetRange/PruneAfter deliberately skip; call it only when corruption is suspected. It is read-only
// and reports every problem it finds in a single pass, returning nil when the durable log is clean.
//
// NOT SAFE FOR CONCURRENT USE with a live StateWAL, or with GetRange/PruneAfter, on the same directory. Call
// it only while no StateWAL is open there (e.g. offline, at startup before New).
func VerifyIntegrity(config *Config) error {
	if err := seiwal.VerifyIntegrity(config.Path); err != nil {
		return fmt.Errorf("state WAL integrity check failed: %w", err)
	}
	return nil
}

func newStateWAL(
	wal seiwal.WAL[[]*proto.NamedChangeSet],
	// The capacity of the channel carrying written blocks to the BUD goroutine.
	budBufferSize uint,
) (StateWAL, error) {
	ctx, cancel := context.WithCancelCause(context.Background())
	w := &stateWALImpl{
		wal:          wal,
		ctx:          ctx,
		cancel:       cancel,
		budChan:      make(chan any, budBufferSize),
		budListeners: newBUDListenerRegistry(),
	}

	// Recover the write-ordering position from the highest block already on disk.
	ok, _, last, err := wal.Bounds()
	if err != nil {
		cancel(nil)
		_ = wal.Close()
		return nil, fmt.Errorf("failed to read WAL bounds: %w", err)
	}
	if ok {
		w.lastBlock.Store(last)
		w.hasBlock = true
		if err := w.seedLastBUD(last); err != nil {
			cancel(nil)
			_ = wal.Close()
			return nil, fmt.Errorf("failed to compute the BUD of the last stored block: %w", err)
		}
	}
	w.closed = utils.MustClose(w, "state WAL")

	w.wg.Add(1)
	go w.budLoop()
	return w, nil
}

// seedLastBUD computes the BUD of the last stored block, the one reported to listeners registered before any
// block is written.
func (w *stateWALImpl) seedLastBUD(
	// The last stored block.
	last uint64,
) error {
	it, err := w.wal.Iterator(last, last)
	if err != nil {
		return fmt.Errorf("failed to create WAL iterator: %w", err)
	}
	defer func() { _ = it.Close() }()

	ok, err := it.Next()
	if err != nil {
		return fmt.Errorf("failed to read block %d: %w", last, err)
	}
	if !ok {
		return fmt.Errorf("block %d is not stored", last)
	}
	_, cs := it.Entry()
	bud, err := computePlaceholderBUD(cs)
	if err != nil {
		return fmt.Errorf("failed to compute the BUD of block %d: %w", last, err)
	}
	w.budListeners.seed(last, bud)
	return nil
}

// Write appends a block's changesets to the WAL as a single record.
func (w *stateWALImpl) Write(blockNumber uint64, cs []*proto.NamedChangeSet) error {
	if w.closed.IsClosed() {
		return fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return fmt.Errorf("state WAL failed: %w", err)
	}
	for i, ncs := range cs {
		if ncs == nil {
			return fmt.Errorf("write rejected: changeset at index %d is nil", i)
		}
	}
	if err := w.checkBlockOrder(blockNumber); err != nil {
		return fmt.Errorf("write rejected: %w", err)
	}

	// Record the new head only once the append succeeds; a failed append bricks the WAL and leaves the
	// head where it was rather than skipping past a block whose changesets were lost.
	if err := w.wal.Append(blockNumber, cs); err != nil {
		return w.fail(fmt.Errorf("failed to append block %d: %w", blockNumber, err))
	}
	w.lastBlock.Store(blockNumber)
	w.hasBlock = true

	if err := w.sendToBUDLoop(budBlock{blockNumber: blockNumber, cs: cs}); err != nil {
		return fmt.Errorf("failed to schedule the BUD of block %d: %w", blockNumber, err)
	}
	return nil
}

// checkBlockOrder rejects a block number that breaks the contiguity rule: the first block written to an
// empty WAL may be any number, and every block after it must be exactly one greater than the last. It
// only reads the write-ordering state, so a Write that fails downstream of it leaves the head untouched.
func (w *stateWALImpl) checkBlockOrder(blockNumber uint64) error {
	if !w.hasBlock {
		return nil
	}
	last := w.lastBlock.Load()
	if blockNumber != last+1 {
		return fmt.Errorf("block number %d is not contiguous with the last block written %d (expected %d)",
			blockNumber, last, last+1)
	}
	return nil
}

// Flush blocks until all previously scheduled writes are durable and their BUDs delivered.
func (w *stateWALImpl) Flush() error {
	if w.closed.IsClosed() {
		return fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return fmt.Errorf("state WAL failed: %w", err)
	}
	if err := w.wal.Flush(); err != nil {
		return w.fail(fmt.Errorf("failed to flush state WAL: %w", err))
	}
	if err := w.awaitBUDs(); err != nil {
		return fmt.Errorf("failed to deliver BUDs: %w", err)
	}
	return nil
}

// RegisterBUDListener adds listener to those each block's BUD is delivered to, and reports the most recent BUD.
func (w *stateWALImpl) RegisterBUDListener(listener BUDListener) (bool, uint64, [32]byte, error) {
	if w.closed.IsClosed() {
		return false, 0, [32]byte{}, fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return false, 0, [32]byte{}, fmt.Errorf("state WAL failed: %w", err)
	}
	ok, blockNumber, bud := w.budListeners.register(listener)
	return ok, blockNumber, bud, nil
}

// GetStoredRange reports the range of complete blocks stored in the WAL.
func (w *stateWALImpl) GetStoredRange() (bool, uint64, uint64, error) {
	if w.closed.IsClosed() {
		return false, 0, 0, fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return false, 0, 0, fmt.Errorf("state WAL failed: %w", err)
	}
	ok, first, last, err := w.wal.Bounds()
	if err != nil {
		return false, 0, 0, w.fail(fmt.Errorf("failed to read WAL bounds: %w", err))
	}
	return ok, first, last, nil
}

// Prune schedules removal of whole underlying files below lowestBlockNumberToKeep. It does not block on
// completion.
func (w *stateWALImpl) Prune(lowestBlockNumberToKeep uint64) error {
	if w.closed.IsClosed() {
		return fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return fmt.Errorf("state WAL failed: %w", err)
	}
	if err := w.wal.PruneBefore(lowestBlockNumberToKeep); err != nil {
		return w.fail(fmt.Errorf("failed to prune state WAL: %w", err))
	}
	return nil
}

// Iterator returns an iterator over the inclusive block range [startingBlockNumber, endingBlockNumber]. It
// yields (blockNumber, changesets) directly from the underlying generic WAL.
func (w *stateWALImpl) Iterator(
	startingBlockNumber uint64,
	endingBlockNumber uint64,
) (StateWALIterator, error) {
	if w.closed.IsClosed() {
		return nil, fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return nil, fmt.Errorf("state WAL failed: %w", err)
	}
	it, err := w.wal.Iterator(startingBlockNumber, endingBlockNumber)
	if err != nil {
		// A rejected range is the caller's error and leaves the WAL usable; only a genuine WAL failure bricks.
		if errors.Is(err, seiwal.ErrIteratorRange) {
			return nil, fmt.Errorf("failed to create WAL iterator: %w", err)
		}
		return nil, w.fail(fmt.Errorf("failed to create WAL iterator: %w", err))
	}
	return &stateWALIterator{Iterator: it}, nil
}

// Close flushes pending writes, closes the underlying WAL, and releases resources. It stops the BUD goroutine
// without waiting for it to deliver the BUDs still queued.
func (w *stateWALImpl) Close() error {
	w.closed.Close(w)
	w.cancel(nil)
	w.wg.Wait()
	if err := w.wal.Close(); err != nil {
		return fmt.Errorf("failed to close state WAL: %w", err)
	}
	return nil
}

// fail records err as the fatal error that bricks the WAL, unless the WAL has already stopped, and returns it.
// Once bricked, every subsequent operation fails fast rather than touching the underlying WAL. Safe to call from
// any goroutine.
func (w *stateWALImpl) fail(err error) error {
	w.cancel(err)
	return err
}

// fatalErr returns the fatal error that bricked the WAL, or nil if it has not been bricked. Safe to call from any
// goroutine.
func (w *stateWALImpl) fatalErr() error {
	// Close() cancels with a nil cause, which context reports as context.Canceled; fail() never passes that bare.
	if cause := context.Cause(w.ctx); cause != nil && cause != context.Canceled {
		return cause
	}
	return nil
}

// stoppedErr describes why the WAL stopped: the fatal error that bricked it, or that it was closed.
func (w *stateWALImpl) stoppedErr() error {
	if err := w.fatalErr(); err != nil {
		return fmt.Errorf("state WAL failed: %w", err)
	}
	return fmt.Errorf("state WAL is closed")
}
