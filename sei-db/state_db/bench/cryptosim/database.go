package cryptosim

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

// Encapsulates the database for the cryptosim benchmark.
type Database struct {
	// The configuration for the benchmark.
	config *CryptoSimConfig

	// The database implementation to use for the benchmark.
	db wrappers.DBWrapper

	// The total number of transactions executed by the benchmark since it last started.
	transactionCount int64

	// A count of the number of transactions in the current batch.
	transactionsInCurrentBlock int64

	// The next block number to be persisted. Tracked internally and incremented after each finalized block.
	nextBlockNumber uint64

	// The writes accumulated for the block currently being assembled, keyed by string(key), already in
	// the form the DB accepts so that finalizing has nothing left to convert.
	//
	// A plain map carrying no synchronization at all, which is sound only because it has one writer at
	// a time and never a concurrent reader. Setup fills it from the main thread before the block
	// builder is started; from then on the builder is the sole writer, harvesting it into each block it
	// publishes. Executors never touch it — they read the frozen map on the block they were handed,
	// which nothing mutates. Letting executors write here instead would put a lock back on the hot
	// path, and that lock is the cost this arrangement exists to remove.
	pendingWrites map[string]*proto.KVPair

	// The block being executed, or nil during setup. Executors read its frozen writes.
	//
	// Written by the main thread before any of that block's transactions are scheduled, and read by
	// executors thereafter. The channel send that schedules a transaction and the flush handshake that
	// ends the block are what order those accesses, so no lock is needed: the write and the reads never
	// overlap.
	currentBlock *block

	// A method that flushes the executors.
	flushFunc func()

	// The metrics for the benchmark.
	metrics *CryptosimMetrics
}

// Creates a new database for the cryptosim benchmark.
func NewDatabase(
	config *CryptoSimConfig,
	db wrappers.DBWrapper,
	metrics *CryptosimMetrics,
	initialNextBlockNumber uint64,
) *Database {
	return &Database{
		config:          config,
		db:              db,
		pendingWrites:   make(map[string]*proto.KVPair),
		metrics:         metrics,
		nextBlockNumber: initialNextBlockNumber,
	}
}

// Insert a key-value pair into the block currently being assembled.
//
// Not safe to call concurrently, with itself or with HarvestWrites — see pendingWrites. Both callers
// are single-threaded and do not overlap: setup on the main thread, and the block builder on its own
// goroutine once setup is done.
//
// The key and value are retained rather than copied, so a caller must not reuse either buffer. Every
// caller allocates both fresh per write.
func (d *Database) Put(key []byte, value []byte) error {
	d.pendingWrites[string(key)] = &proto.KVPair{Key: key, Value: value}
	return nil
}

// HarvestWrites returns the writes accumulated since the last harvest and installs a fresh map for
// the next block. The returned map must not be modified: the block it is handed to publishes it to
// the executors, who read it without synchronization.
//
// Called only by the block builder, on its own goroutine, between blocks.
func (d *Database) HarvestWrites() map[string]*proto.KVPair {
	harvested := d.pendingWrites
	d.pendingWrites = make(map[string]*proto.KVPair, len(harvested))
	return harvested
}

// SetCurrentBlock records the block whose transactions are about to be scheduled, so reads can see
// the writes that block makes. Called by the main thread before any of that block's transactions is
// handed to an executor.
func (d *Database) SetCurrentBlock(blk *block) {
	d.currentBlock = blk
}

// Retrieve a value from the database.
//
// Every read goes to the DB. There is deliberately no in-memory short-circuit in front of it: the
// read throughput of the DB is the thing this benchmark exists to measure, so a read served from a
// map is a read that did not get measured. An earlier version consulted the pending writes first,
// which silently excluded most of a block's reads from the measurement, because a transaction reads
// the same keys it writes.
func (d *Database) Get(key []byte) ([]byte, bool, error) {
	value, found, err := d.db.Read(key)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read from database: %w", err)
	}
	if found {
		return value, true, nil
	}

	return nil, false, nil
}

// Signal that a transaction has been added to the current block.
func (d *Database) IncrementTransactionCount() {
	d.AddTransactionCount(1)
}

// Signal that count transactions have been added to the current block.
func (d *Database) AddTransactionCount(count int64) {
	d.transactionCount += count
	d.transactionsInCurrentBlock += count
}

// Reset the transaction count. Useful for when changing test phases.
func (d *Database) ResetTransactionCount() {
	d.transactionCount = 0
	d.transactionsInCurrentBlock = 0
}

// Get the total number of transactions executed by the benchmark since it last started.
func (d *Database) TransactionCount() int64 {
	return d.transactionCount
}

// Commit the current batch if it has reached the configured number of transactions.
// Returns true if the batch was finalized, false if not.
func (d *Database) MaybeFinalizeBlock(
	nextAccountID int64,
	nextErc20ContractID int64,
) (bool, error) {
	if d.transactionsInCurrentBlock >= int64(d.config.TransactionsPerBlock) {
		err := d.FinalizeBlock(nextAccountID, nextErc20ContractID)
		if err != nil {
			return false, fmt.Errorf("failed to finalize block: %w", err)
		}
		return true, nil
	}
	return false, nil
}

// Push the current block out to the database.
func (d *Database) FinalizeBlock(
	nextAccountID int64,
	nextErc20ContractID int64,
) error {

	d.metrics.SetMainThreadPhase("execute_block")

	// Wait for all transactions in the current block to be executed.
	if d.flushFunc != nil {
		d.flushFunc()
	}

	if d.transactionsInCurrentBlock == 0 {
		return nil
	}

	d.metrics.SetMainThreadPhase("finalizing")

	// One changeset carrying every pair, matching the shape a real block produces: sei-cosmos emits
	// one NamedChangeSet per module, so the evm module's whole block arrives as a single contiguous
	// batch of pairs. Wrapping each pair in its own changeset instead would make the consuming store
	// chase a separate allocation per pair, which is benchmark overhead rather than a real cost.
	pairs := d.blockPairs()

	// Persist the account ID counter in every batch.
	nonceValue := make([]byte, 8)
	//nolint:gosec // G115 - nextAccountID is benchmark counter, overflow acceptable
	binary.BigEndian.PutUint64(nonceValue, uint64(nextAccountID))
	pairs = append(pairs, &proto.KVPair{Key: AccountIDCounterKey(), Value: nonceValue})

	// Persist the ERC20 contract ID counter in every batch.
	erc20ContractIDValue := make([]byte, 8)
	//nolint:gosec // G115 - nextErc20ContractID is benchmark counter, overflow acceptable
	binary.BigEndian.PutUint64(erc20ContractIDValue, uint64(nextErc20ContractID))
	pairs = append(pairs, &proto.KVPair{Key: Erc20IDCounterKey(), Value: erc20ContractIDValue})

	// Persist the block number counter in every batch.
	blockNumberValue := make([]byte, 8)
	binary.BigEndian.PutUint64(blockNumberValue, d.nextBlockNumber)
	pairs = append(pairs, &proto.KVPair{Key: BlockNumberCounterKey(), Value: blockNumberValue})
	d.nextBlockNumber++

	entry := &proto.ChangelogEntry{
		Version: d.db.Version() + 1,
		Changesets: []*proto.NamedChangeSet{{
			Name:      wrappers.EVMStoreName,
			Changeset: proto.ChangeSet{Pairs: pairs},
		}},
	}
	err := d.db.ApplyChangeSets(entry)
	if err != nil {
		return fmt.Errorf("failed to apply change sets: %w", err)
	}

	d.metrics.ReportBlockFinalized(d.transactionsInCurrentBlock)
	d.transactionsInCurrentBlock = 0

	// One commit per block: that is the store contract, so the benchmark must not batch.
	d.metrics.SetMainThreadPhase("committing")
	version, err := d.db.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}
	d.metrics.ReportDBCommit()

	if err := d.awaitLaggingHash(version); err != nil {
		return err
	}

	d.metrics.SetMainThreadPhase("executing")

	return nil
}

// blockPairs returns the block's writes in the form the DB accepts, without the counter keys, which
// FinalizeBlock appends.
//
// There are two sources because there are two producers. A benchmark block arrives with its pairs
// already built by the block builder, so this is a field read and the conversion cost has already
// been paid off the critical path — the point of the whole arrangement. Setup has no block: it Puts
// account and contract data straight into pendingWrites, and there is nowhere earlier to have done
// the conversion, so it happens here. Setup runs once and is not what the benchmark reports.
func (d *Database) blockPairs() []*proto.KVPair {
	if d.currentBlock != nil {
		return d.currentBlock.Changeset()
	}

	pairs := make([]*proto.KVPair, 0, len(d.pendingWrites)+3)
	for _, pair := range d.pendingWrites {
		pairs = append(pairs, pair)
	}
	d.pendingWrites = make(map[string]*proto.KVPair)
	return pairs
}

// awaitLaggingHash waits for the hash of the block HashAsynchrony blocks behind the one just committed,
// which is what consumes the database's hash stream.
//
// Time spent here is time hashing could not keep up with execution: the hash asked for is one the hasher has
// had HashAsynchrony blocks to produce, so with any slack at all the wait is free.
func (d *Database) awaitLaggingHash(version int64) error {
	target := version - d.config.HashAsynchrony
	if target < 1 {
		// The chain is not that long yet, so there is nothing behind us to wait for.
		return nil
	}

	d.metrics.SetMainThreadPhase("awaiting_hash")
	if err := d.db.AwaitBlockHash(target); err != nil {
		return fmt.Errorf("failed to await hash of block %d: %w", target, err)
	}
	return nil
}

// Close the database and release any resources.
func (d *Database) Close(nextAccountID int64, nextErc20ContractID int64) error {
	fmt.Printf("Committing final batch.\n")

	if err := d.FinalizeBlock(nextAccountID, nextErc20ContractID); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}

	fmt.Printf("Closing database.\n")
	err := d.db.Close()
	if err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	return nil
}

// Close the database and release any resources without finalizing the last batch.
func (d *Database) CloseWithoutFinalizing() error {
	fmt.Printf("Closing database.\n")
	err := d.db.Close()
	if err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	return nil
}

// Set the function that flushes the executors. This setter is required to break a circular dependency.
func (d *Database) SetFlushFunc(flushFunc func()) {
	d.flushFunc = flushFunc
}
