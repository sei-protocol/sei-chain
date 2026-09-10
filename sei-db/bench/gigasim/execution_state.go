package gigasim

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// executionState is the state DB as the execution phase sees it: a read view of the last committed
// block, and a batch collecting the writes of the block being executed.
//
// The state DB itself belongs to the storage manager, which opened it and closes it. Only the view
// opened here is released by Close.
type executionState struct {
	// The state DB the storage manager opened, holding the state commit store, the EVM state store
	// and the state WAL.
	db *giga.StateDB

	// A read-only view of the most recently committed block, which every read that misses the current
	// batch is served from. Replaced after each commit.
	view gigatypes.StateView

	// The writes of the block currently executing, keyed by the raw EVM key. Written concurrently by
	// the executors and drained by the main thread at commit.
	batch *stateBatch

	// Takes one block hash per block committed, so that the benchmark cannot outrun hashing.
	hashes *blockHashWaiter

	metrics *GigasimMetrics
}

// newExecutionState opens a read view over the state DB and registers the hash listener that paces
// the benchmark against hashing.
func newExecutionState(
	config *GigasimConfig,
	db *giga.StateDB,
	metrics *GigasimMetrics,
) (*executionState, error) {
	view := db.OpenView()

	// Registered before the first block is committed, which is the only point a listener can be sure
	// of being handed every block's hash.
	waiter := newBlockHashWaiter(config.HashLagBlocks, metrics)
	if _, err := db.RegisterHashListener(waiter.listen); err != nil {
		view.Close()
		return nil, fmt.Errorf("failed to register a block hash listener: %w", err)
	}

	return &executionState{
		db:      db,
		view:    view,
		batch:   newStateBatch(),
		hashes:  waiter,
		metrics: metrics,
	}, nil
}

// height returns the block the read view was opened at, which is the newest block the state DB holds.
func (s *executionState) height() int64 {
	return s.view.GetBlockHeight()
}

// Put stages a write for the block currently executing.
//
// Safe to call concurrently with other calls to Put and Get, but not with commitBlock.
func (s *executionState) Put(key []byte, value []byte) {
	s.batch.Put(key, value)
}

// Get reads a key, answering from the block currently executing before falling back to the view.
//
// Safe to call concurrently with other calls to Put and Get, but not with commitBlock.
func (s *executionState) Get(key []byte) ([]byte, bool) {
	if value, found := s.batch.Get(key); found {
		return value, true
	}
	return s.view.Get(keys.EVMStoreKey, key)
}

// commitBlock writes the staged batch to the state DB as blockNum, reopens the read view over it, and
// waits for a block hash once the benchmark is a full lag window ahead of hashing. The identifier
// counters ride along, so that a reopened data directory resumes where the previous run stopped.
//
// Must not run concurrently with Put or Get.
func (s *executionState) commitBlock(blockNum int64, counters identifierCounters) error {
	s.metrics.SetMainThreadPhase("finalizing")

	changeSets := s.batch.drainToChangeSet(counters)

	// One commit per block: that is the store contract, so the benchmark must not batch.
	s.metrics.SetMainThreadPhase("committing_state")
	if err := s.db.CommitStateChanges(blockNum, changeSets); err != nil {
		return fmt.Errorf("failed to commit block %d to the state DB: %w", blockNum, err)
	}
	s.metrics.ReportStateCommit(int64(len(changeSets[0].Changeset.Pairs)))
	s.reopenView()

	// Committing a block is not finishing it: the hash of a block committed a bounded number of blocks
	// ago is taken here, and waited for when hashing has fallen behind execution.
	if err := s.hashes.awaitBlock(); err != nil {
		return fmt.Errorf("failed to obtain a block hash after committing block %d: %w", blockNum, err)
	}
	return nil
}

// reopenView replaces the read view with one over the block just committed. A view never observes
// writes made after it was opened, so without this every read would keep answering from the height
// the benchmark started at.
func (s *executionState) reopenView() {
	s.view.Close()
	s.view = s.db.OpenView()
}

// Close releases the read view. The state DB below it stays open: the storage manager owns it.
func (s *executionState) Close() {
	s.view.Close()
}

// encodeCounter renders an identifier counter as the fixed-width value the counter keys hold.
func encodeCounter(value int64) []byte {
	encoded := make([]byte, 8)
	//nolint:gosec // G115 - benchmark counter, overflow acceptable
	binary.BigEndian.PutUint64(encoded, uint64(value))
	return encoded
}
