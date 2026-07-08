package statewal

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

var _ StateWAL = (*stateWALImpl)(nil)

// A WAL for storing state changesets by block number.
type stateWALImpl struct {
	// The underlying generic WAL, keyed by block number, whose payload is a block's changesets.
	wal seiwal.WAL[[]*proto.NamedChangeSet]

	// Set by Close() so subsequent Write/SignalEndOfBlock calls fail fast.
	closed atomic.Bool

	// Guards the write-ordering contract state and the accumulation buffer below.
	mu sync.Mutex
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

// NewWithRollback opens a state WAL and deletes all data for blocks beyond rollbackBlockNumber before
// returning, so the WAL contains no block greater than rollbackBlockNumber.
func NewWithRollback(config *Config, rollbackBlockNumber uint64) (StateWAL, error) {
	wal, err := seiwal.NewGenericWALWithRollback[[]*proto.NamedChangeSet](
		config.toSeiwalConfig(), rollbackBlockNumber, serializeChangesets, deserializeChangesets)
	if err != nil {
		return nil, fmt.Errorf("failed to open state WAL: %w", err)
	}
	return newStateWAL(wal)
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
	if w.closed.Load() {
		return fmt.Errorf("state WAL is closed")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.enforceWriteOrdering(blockNumber); err != nil {
		return fmt.Errorf("write rejected: %w", err)
	}
	w.buf = append(w.buf, cs...)
	return nil
}

// SignalEndOfBlock appends the current block's accumulated changesets to the WAL as a single record.
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
	changeset := w.buf
	w.buf = nil // hand ownership to the WAL; the next block starts a fresh buffer
	w.mu.Unlock()

	if err := w.wal.Append(blockNumber, changeset); err != nil {
		return fmt.Errorf("failed to append block %d: %w", blockNumber, err)
	}
	return nil
}

// enforceWriteOrdering rejects a Write that violates the block-ordering rules (no decreasing block numbers; no
// advancing to a new block before the current one is ended) and records the new position when it is allowed.
// The caller must hold w.mu.
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
	w.currentBlock = blockNumber
	w.currentBlockEnded = false
	return nil
}

// Flush blocks until all previously scheduled writes are durable.
func (w *stateWALImpl) Flush() error {
	return w.wal.Flush()
}

// GetStoredRange reports the range of complete blocks stored in the WAL.
func (w *stateWALImpl) GetStoredRange() (bool, uint64, uint64, error) {
	return w.wal.Bounds()
}

// Prune schedules removal of whole underlying files below lowestBlockNumberToKeep. It does not block on
// completion.
func (w *stateWALImpl) Prune(lowestBlockNumberToKeep uint64) error {
	return w.wal.Prune(lowestBlockNumberToKeep)
}

// Iterator returns an iterator over the WAL starting at startingBlockNumber. It yields (blockNumber,
// changesets) directly from the underlying generic WAL.
func (w *stateWALImpl) Iterator(startingBlockNumber uint64) (seiwal.Iterator[[]*proto.NamedChangeSet], error) {
	return w.wal.Iterator(startingBlockNumber)
}

// Close flushes pending writes, closes the underlying WAL, and releases resources.
func (w *stateWALImpl) Close() error {
	w.closed.Store(true)
	return w.wal.Close()
}
