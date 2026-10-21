package statewal

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

var _ StateWAL = (*stateWALImpl)(nil)

// A WAL for storing each block's state mutations by block number.
//
// Not safe for concurrent use; see the StateWAL interface doc. The gc.PrunableStore surface in
// state_wal_gc.go is the exception: it runs on the collector's goroutine, and touches only lastBlock and
// the WAL underneath.
type stateWALImpl struct {
	// The underlying generic WAL, keyed by block number, whose payload is a block's record.
	wal seiwal.WAL[stateWALRecord]

	// Closed by Close() so subsequent calls fail fast.
	closed utils.CloseMarker[stateWALImpl]

	// Cancelled when the WAL stops: by fail() with the fatal error that bricked the WAL as its cause, or by
	// Close() with a nil cause. Every blocking operation aborts on it, and it is the ctx passed to hash listeners.
	ctx context.Context

	// Cancels ctx, recording the fatal error (or nil) as its cause. Only the first call takes effect.
	cancel context.CancelCauseFunc

	// Computes each written block's hash and delivers it to the listeners.
	hasher *stateWALHasher

	// Sorts each written block's mutations and hands them to wal and hasher. Every request to either that must stay
	// ordered with writes passes through it.
	sorter *stateWALSorter

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
	wal, err := seiwal.NewGenericWAL[stateWALRecord](config.toSeiwalConfig(), serializeRecord, deserializeRecord)
	if err != nil {
		return nil, fmt.Errorf("failed to open state WAL: %w", err)
	}
	return newStateWAL(wal, config.HasherBufferSize, config.SortBufferSize)
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
	wal seiwal.WAL[stateWALRecord],
	// The capacity of the channel carrying hash jobs to the hasher.
	hasherBufferSize uint,
	// The capacity of the channel carrying requests to the sorter.
	sortBufferSize uint,
) (StateWAL, error) {
	// Recover the write-ordering position, and the hash reported to listeners registered before any block is
	// written, from the highest block already on disk.
	ok, _, last, err := wal.Bounds()
	if err != nil {
		_ = wal.Close()
		return nil, fmt.Errorf("failed to read WAL bounds: %w", err)
	}
	var lastHash [32]byte
	if ok {
		lastHash, err = computeStoredHash(wal, last)
		if err != nil {
			_ = wal.Close()
			return nil, fmt.Errorf("failed to compute the hash of the last stored block: %w", err)
		}
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	// Neither may reference w: their goroutines would keep w reachable, and MustClose() could never fire.
	hasher := newStateWALHasher(ctx, cancel, hasherBufferSize, ok, last, lastHash)
	w := &stateWALImpl{
		wal:    wal,
		ctx:    ctx,
		cancel: cancel,
		hasher: hasher,
		sorter: newStateWALSorter(ctx, cancel, sortBufferSize, wal, hasher),
	}
	if ok {
		w.lastBlock.Store(last)
		w.hasBlock = true
	}
	w.closed = utils.MustClose(w, "state WAL")
	return w, nil
}

// computeStoredHash returns the hash of a block already stored in wal.
func computeStoredHash(
	// The WAL holding the block.
	wal seiwal.WAL[stateWALRecord],
	// The block to compute the hash of.
	blockNumber uint64,
) ([32]byte, error) {
	it, err := wal.Iterator(blockNumber, blockNumber)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to create WAL iterator: %w", err)
	}
	defer func() { _ = it.Close() }()

	ok, err := it.Next()
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to read block %d: %w", blockNumber, err)
	}
	if !ok {
		return [32]byte{}, fmt.Errorf("block %d is not stored", blockNumber)
	}
	_, record := it.Entry()
	return record.hash(), nil
}

// Write appends a block's mutations to the WAL as a single record.
func (w *stateWALImpl) Write(blockNumber uint64, mutations []mutation.Mutation) error {
	if w.closed.IsClosed() {
		return fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return fmt.Errorf("state WAL failed: %w", err)
	}
	if err := w.checkBlockOrder(blockNumber); err != nil {
		return fmt.Errorf("write rejected: %w", err)
	}

	if !w.sorter.ScheduleSort(blockNumber, mutations) {
		return fmt.Errorf("failed to schedule block %d: %w", blockNumber, w.stoppedErr())
	}
	w.lastBlock.Store(blockNumber)
	w.hasBlock = true
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

// Flush blocks until all previously scheduled writes are durable and their hashes delivered.
func (w *stateWALImpl) Flush() error {
	if w.closed.IsClosed() {
		return fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return fmt.Errorf("state WAL failed: %w", err)
	}
	if err := w.sorter.Flush(); err != nil {
		return w.fail(err)
	}
	return nil
}

// RegisterHashListener adds listener to those each block's hash is delivered to, and reports the most recent hash.
func (w *stateWALImpl) RegisterHashListener(listener HashListener) (bool, uint64, [32]byte, error) {
	if w.closed.IsClosed() {
		return false, 0, [32]byte{}, fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return false, 0, [32]byte{}, fmt.Errorf("state WAL failed: %w", err)
	}
	ok, blockNumber, hash, accepted := w.sorter.RegisterListener(listener)
	if !accepted {
		return false, 0, [32]byte{}, w.stoppedErr()
	}
	return ok, blockNumber, hash, nil
}

// GetStoredRange reports the range of complete blocks stored in the WAL.
func (w *stateWALImpl) GetStoredRange() (bool, uint64, uint64, error) {
	if w.closed.IsClosed() {
		return false, 0, 0, fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return false, 0, 0, fmt.Errorf("state WAL failed: %w", err)
	}
	ok, first, last, err := w.sorter.Bounds()
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
	if err := w.sorter.Prune(lowestBlockNumberToKeep); err != nil {
		return w.fail(fmt.Errorf("failed to prune state WAL: %w", err))
	}
	return nil
}

// Iterator returns an iterator over the inclusive block range [startingBlockNumber, endingBlockNumber]. It
// yields (blockNumber, changesets) for each block, whichever format its record is stored in.
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
	it, err := w.sorter.Iterator(startingBlockNumber, endingBlockNumber)
	if err != nil {
		// A rejected range is the caller's error and leaves the WAL usable; only a genuine WAL failure bricks.
		if errors.Is(err, seiwal.ErrIteratorRange) {
			return nil, fmt.Errorf("failed to create WAL iterator: %w", err)
		}
		return nil, w.fail(fmt.Errorf("failed to create WAL iterator: %w", err))
	}
	return &stateWALIterator{records: it}, nil
}

// Close blocks until all work submitted before the call has been fully handled, then closes the underlying WAL.
func (w *stateWALImpl) Close() error {
	w.closed.Close(w)
	w.sorter.ScheduleClose()
	w.sorter.AwaitExit()
	w.hasher.AwaitExit()
	w.cancel(nil)
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
