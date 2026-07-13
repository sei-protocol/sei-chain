package statewal

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

var _ StateWAL = (*stateWALImpl)(nil)

// A WAL for storing state changesets by block number.
//
// Not safe for concurrent use; see the StateWAL interface doc.
type stateWALImpl struct {
	// The underlying generic WAL, keyed by block number, whose payload is a block's changesets.
	wal seiwal.WAL[[]*proto.NamedChangeSet]

	// Set by Close() so subsequent calls fail fast. A plain field: like the write-ordering state below, it
	// is only ever touched by the single caller, which must not invoke methods concurrently.
	closed bool

	// The first fatal error from the underlying WAL that bricked this one, surfaced to the caller by every
	// subsequent operation. Once set, no operation touches the underlying WAL, so a corrupt WAL never
	// limps onward. Caller-serialized like closed.
	fatalErr error

	// The write-ordering contract state and the accumulation buffer below are mutated by Write and
	// SignalEndOfBlock, which callers must not invoke concurrently.

	// The block number of the most recent Write or SignalEndOfBlock.
	currentBlock uint64
	// Whether currentBlock has been finalized by SignalEndOfBlock.
	currentBlockEnded bool
	// Whether any block has been observed (this session or recovered from disk).
	hasCurrentBlock bool
	// The changesets accumulated for the current block across its Write calls, appended as one record at
	// end-of-block. Ownership is handed to the WAL at end-of-block and a fresh buffer starts for the next
	// block, so the serialization goroutine never races the wrapper over the backing array.
	buf []*proto.NamedChangeSet
}

// New opens (or creates) a state WAL in the configured directory, recovering any files left behind by a
// previous session.
func New(config *Config) (StateWAL, error) {
	wal, err := seiwal.NewGenericWAL[[]*proto.NamedChangeSet](
		config.toSeiwalConfig(), serializeChangesets, deserializeChangesets)
	if err != nil {
		return nil, fmt.Errorf("failed to open state WAL: %w", err)
	}
	return newStateWAL(wal)
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

func newStateWAL(wal seiwal.WAL[[]*proto.NamedChangeSet]) (StateWAL, error) {
	w := &stateWALImpl{wal: wal}

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
	return w, nil
}

// Write accumulates a set of changes for the given block number in memory.
func (w *stateWALImpl) Write(blockNumber uint64, cs []*proto.NamedChangeSet) error {
	if w.closed {
		return fmt.Errorf("state WAL is closed")
	}
	if w.fatalErr != nil {
		return fmt.Errorf("state WAL failed: %w", w.fatalErr)
	}
	if err := w.enforceWriteOrdering(blockNumber); err != nil {
		return fmt.Errorf("write rejected: %w", err)
	}
	w.buf = append(w.buf, cs...)
	return nil
}

// SignalEndOfBlock appends the current block's accumulated changesets to the WAL as a single record.
func (w *stateWALImpl) SignalEndOfBlock() error {
	if w.closed {
		return fmt.Errorf("state WAL is closed")
	}
	if w.fatalErr != nil {
		return fmt.Errorf("state WAL failed: %w", w.fatalErr)
	}

	if !w.hasCurrentBlock || w.currentBlockEnded {
		return fmt.Errorf("no block in progress to end")
	}

	// Commit the finalization state only after the append succeeds; a failed append bricks the WAL and
	// leaves the block in progress rather than silently finalizing a block whose changesets were lost.
	if err := w.wal.Append(w.currentBlock, w.buf); err != nil {
		return w.fail(fmt.Errorf("failed to append block %d: %w", w.currentBlock, err))
	}
	w.currentBlockEnded = true
	w.buf = nil // hand ownership to the WAL; the next block starts a fresh buffer
	return nil
}

// enforceWriteOrdering rejects a Write that violates the block-ordering rules (no decreasing block numbers; no
// advancing to a new block before the current one is ended) and records the new position when it is allowed.
func (w *stateWALImpl) enforceWriteOrdering(blockNumber uint64) error {
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
	if blockNumber != w.currentBlock+1 {
		return fmt.Errorf("block number %d is not contiguous with the current block number %d (expected %d)",
			blockNumber, w.currentBlock, w.currentBlock+1)
	}
	w.currentBlock = blockNumber
	w.currentBlockEnded = false
	return nil
}

// Flush blocks until all previously scheduled writes are durable.
func (w *stateWALImpl) Flush() error {
	if w.closed {
		return fmt.Errorf("state WAL is closed")
	}
	if w.fatalErr != nil {
		return fmt.Errorf("state WAL failed: %w", w.fatalErr)
	}
	if err := w.wal.Flush(); err != nil {
		return w.fail(fmt.Errorf("failed to flush state WAL: %w", err))
	}
	return nil
}

// GetStoredRange reports the range of complete blocks stored in the WAL.
func (w *stateWALImpl) GetStoredRange() (bool, uint64, uint64, error) {
	if w.closed {
		return false, 0, 0, fmt.Errorf("state WAL is closed")
	}
	if w.fatalErr != nil {
		return false, 0, 0, fmt.Errorf("state WAL failed: %w", w.fatalErr)
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
	if w.closed {
		return fmt.Errorf("state WAL is closed")
	}
	if w.fatalErr != nil {
		return fmt.Errorf("state WAL failed: %w", w.fatalErr)
	}
	if err := w.wal.PruneBefore(lowestBlockNumberToKeep); err != nil {
		return w.fail(fmt.Errorf("failed to prune state WAL: %w", err))
	}
	return nil
}

// Iterator returns an iterator over the WAL starting at startingBlockNumber. It yields (blockNumber,
// changesets) directly from the underlying generic WAL.
func (w *stateWALImpl) Iterator(startingBlockNumber uint64) (seiwal.Iterator[[]*proto.NamedChangeSet], error) {
	if w.closed {
		return nil, fmt.Errorf("state WAL is closed")
	}
	if w.fatalErr != nil {
		return nil, fmt.Errorf("state WAL failed: %w", w.fatalErr)
	}
	it, err := w.wal.Iterator(startingBlockNumber)
	if err != nil {
		return nil, w.fail(fmt.Errorf("failed to create WAL iterator: %w", err))
	}
	return it, nil
}

// Close flushes pending writes, closes the underlying WAL, and releases resources.
func (w *stateWALImpl) Close() error {
	w.closed = true
	if err := w.wal.Close(); err != nil {
		return fmt.Errorf("failed to close state WAL: %w", err)
	}
	return nil
}

// fail records err as the first fatal error that bricks the WAL and returns it. Once set, every
// subsequent operation fails fast rather than touching the underlying WAL.
func (w *stateWALImpl) fail(err error) error {
	if w.fatalErr == nil {
		w.fatalErr = err
	}
	return err
}
