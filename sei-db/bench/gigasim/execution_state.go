package gigasim

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// executionState is the state DB as the execution phase sees it: a read view of the last committed
// block, and the writes setup stages before generation takes over.
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

	// The writes setup stages, drained as one block by finalizeSetupBlock. A measured run never uses
	// this: the generator stages a block's writes when it builds the block.
	setupWrites *stateBatch

	// Takes one block hash per block committed, so that the benchmark cannot outrun hashing.
	hashes *blockHashWaiter

	// The main loop's share of a block's critical path, shared with the run loop because both run on
	// that goroutine and a timer tracks one goroutine's current phase.
	lifecycle *metrics.PhaseTimer

	metrics *GigasimMetrics
}

// newExecutionState opens a read view over the state DB and registers the hash listener that paces
// the benchmark against hashing.
func newExecutionState(
	config *GigasimConfig,
	db *giga.StateDB,
	metrics *GigasimMetrics,
	lifecycle *metrics.PhaseTimer,
) (*executionState, error) {
	view := db.OpenView()

	// Registered before the first block is committed, which is the only point a listener can be sure
	// of being handed every block's hash.
	waiter := newBlockHashWaiter(config.MaxHashLagBlocks, metrics)
	if _, err := db.RegisterHashListener(waiter.listen); err != nil {
		view.Close()
		return nil, fmt.Errorf("failed to register a block hash listener: %w", err)
	}

	return &executionState{
		db:          db,
		view:        view,
		setupWrites: newStateBatch(config.TransactionsPerBlock),
		hashes:      waiter,
		lifecycle:   lifecycle,
		metrics:     metrics,
	}, nil
}

// height returns the block the read view was opened at, which is the newest block the state DB holds.
func (s *executionState) height() int64 {
	return s.view.GetBlockHeight()
}

// Put stages a write for the setup block being assembled. Setup is the only caller: during a run the
// generator stages a block's writes, so nothing writes through here while the executors are running.
func (s *executionState) Put(key []byte, value []byte) {
	s.setupWrites.Put(key, value)
}

// drainSetupWrites returns what setup has staged as the block to commit, leaving the batch empty for
// the next one.
func (s *executionState) drainSetupWrites(counters identifierCounters) blockWrites {
	return s.setupWrites.drainToChangeSet(counters)
}

// Get reads a key from the newest committed block.
//
// Every read goes to the state DB. There is deliberately nothing in memory in front of it: the read
// throughput of the DB is what this benchmark exists to measure, so a read answered from a map is a
// read that did not get measured. Answering from the block being executed cost most of that
// measurement, because every transaction reads the fee account and every transaction writes it.
//
// Safe to call concurrently with other calls to Get, but not with commitBlock.
func (s *executionState) Get(key []byte) ([]byte, bool) {
	return s.view.Get(keys.EVMStoreKey, key)
}

// commitBlock writes a block's changeset to the state DB as blockNum, reopens the read view over it,
// and waits for a block hash once the benchmark is a full lag window ahead of hashing.
//
// The changeset arrives already assembled — by the generator during a run, by setup before one — so
// this performs no work of its own ahead of the commit.
//
// Must not run concurrently with Put or Get.
func (s *executionState) commitBlock(blockNum int64, writes blockWrites) error {
	// SC and SS are handed the same changeset, so the volume they take in is the same. SS is reported
	// only when it is open, which is what makes it fall to zero on a validator's stack rather than
	// claiming writes nothing performed.
	s.metrics.ReportStoreBytesWritten(storeStateCommit, writes.bytes)
	if s.db.SS() != nil {
		s.metrics.ReportStoreBytesWritten(storeStateStore, writes.bytes)
	}

	// One commit per block: that is the store contract, so the benchmark must not batch.
	// The state DB splits the commit across the state WAL, SC and SS and times each itself, being the
	// layer that can tell them apart. Standing down here keeps one commit out of two breakdowns.
	s.lifecycle.Reset()
	if err := s.db.CommitStateChanges(blockNum, writes.changeSets); err != nil {
		return fmt.Errorf("failed to commit block %d to the state DB: %w", blockNum, err)
	}
	s.metrics.ReportStateCommit(int64(len(writes.changeSets[0].Changeset.Pairs)))

	// Committing a block is not finishing it: the hash of a block committed a bounded number of blocks
	// ago is taken here, and waited for when hashing has fallen behind execution. Reopening the view
	// is charged here too, being a fraction of a percent that no one reads as a stage of its own.
	s.lifecycle.SetPhase("await_hash")
	s.reopenView()
	err := s.hashes.awaitBlock()
	if err != nil {
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
