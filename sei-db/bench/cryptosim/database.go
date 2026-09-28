package cryptosim

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// The number of counter keys FinalizeBlock appends to every block's changeset: the account ID counter
// and the ERC20 contract ID counter.
const counterKeysPerBlock = 2

// Encapsulates the database for the cryptosim benchmark.
type Database struct {
	// The configuration for the benchmark.
	config *CryptoSimConfig

	// The database implementation to use for the benchmark.
	db gigatypes.StateDB

	// Enforces retention across the stores the database opened.
	garbageCollector *controller.StorageGarbageCollector

	// A read-only view of the most recently committed block, which every read that misses the
	// current batch is served from. Replaced after each commit.
	view gigatypes.StateView

	// The total number of transactions executed by the benchmark since it last started.
	transactionCount int64

	// A count of the number of transactions in the current batch.
	transactionsInCurrentBlock int64

	// The block number the next commit lands on. Incremented after each finalized block.
	nextBlockNumber int64

	// The writes accumulated for the block currently being assembled, keyed by string(key), already in
	// the form the DB accepts so that finalizing has nothing left to convert.
	//
	// A plain map carrying no synchronization at all, which is sound only because it has one writer at a
	// time and never a concurrent reader. Setup fills it from the main thread before the block builder
	// is started; from then on the builder is the sole writer, harvesting it into each block it
	// publishes. Executors never touch it — they write nothing, and their reads go to the DB.
	pendingWrites map[string]*proto.KVPair

	// The block being executed, or nil during setup. The DB reads its frozen changeset when the block
	// is finalized.
	//
	// Written by the main thread before any of that block's transactions are scheduled, and read by the
	// finalize path on that same thread, so no lock is needed.
	currentBlock *block

	// A method that flushes the executors.
	flushFunc func()

	// The metrics for the benchmark.
	metrics *CryptosimMetrics

	// Takes one block hash per block committed, so that the benchmark cannot outrun hashing.
	hashes *blockHashWaiter
}

// Creates a new database for the cryptosim benchmark.
func NewDatabase(
	config *CryptoSimConfig,
	db gigatypes.StateDB,
	garbageCollector *controller.StorageGarbageCollector,
	metrics *CryptosimMetrics,
) (*Database, error) {
	// The view is both what reads are served from and where the starting height comes from: the
	// store accepts only the block after the one it opened at.
	view := db.OpenView()
	database := &Database{
		config:           config,
		db:               db,
		garbageCollector: garbageCollector,
		view:             view,
		pendingWrites:    make(map[string]*proto.KVPair),
		metrics:          metrics,
		nextBlockNumber:  view.GetBlockHeight() + 1,
	}

	// Registered here because this is before the first block is committed, and that is the only place
	// a listener can be sure of being handed every block's hash.
	waiter := newBlockHashWaiter(config.HashLagBlocks, metrics)
	if _, err := db.RegisterHashListener(waiter.listen); err != nil {
		view.Close()
		return nil, fmt.Errorf("failed to register a block hash listener: %w", err)
	}
	database.hashes = waiter
	return database, nil
}

// Insert a key-value pair into the block currently being assembled.
//
// Not safe to call concurrently, with itself or with HarvestWrites() — see pendingWrites. Both callers
// are single-threaded and do not overlap: setup on the main thread, and the block builder on its own
// goroutine once setup is done.
//
// The key and value are retained rather than copied, so a caller must not reuse either buffer. Every
// caller allocates both fresh per write, or takes them from the immutable canned random buffer.
func (d *Database) Put(key []byte, value []byte) error {
	d.pendingWrites[string(key)] = &proto.KVPair{Key: key, Value: value}
	return nil
}

// HarvestWrites returns the writes accumulated since the last harvest and installs a fresh map for the
// next block. The returned map must not be modified once it has been handed to a block.
//
// Called only by the block builder, on its own goroutine, between blocks.
func (d *Database) HarvestWrites() map[string]*proto.KVPair {
	harvested := d.pendingWrites
	d.pendingWrites = make(map[string]*proto.KVPair, len(harvested))
	return harvested
}

// SetCurrentBlock records the block whose transactions are about to be scheduled, so that finalizing
// commits that block's changeset. Called by the main thread before any of that block's transactions is
// handed to an executor.
func (d *Database) SetCurrentBlock(blk *block) {
	d.currentBlock = blk
}

// Retrieve a value from the database.
//
// Every read goes to the DB. There is deliberately no in-memory short-circuit in front of it: the read
// throughput of the DB is the thing this benchmark exists to measure, so a read served from a map is a
// read that did not get measured. A transaction reads the same keys it writes, so consulting the
// block's writes first silently excluded most of a block's reads from the measurement.
//
// This method is safe to call concurrently with other calls to Get(). Is not thread safe with
// FinalizeBlock().
func (d *Database) Get(key []byte) ([]byte, bool) {
	return d.view.Get(keys.EVMStoreKey, key)
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

	// One changeset carrying every pair, matching the shape a real block produces: the EVM module's
	// whole block arrives as a single contiguous batch of pairs. Wrapping each pair in a changeset of
	// its own instead would make the consuming store chase a separate allocation per pair, which is
	// benchmark overhead rather than a real cost.
	changeSets := []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: pairs},
	}}

	blockNum := d.nextBlockNumber

	d.metrics.ReportBlockFinalized(d.transactionsInCurrentBlock)
	d.transactionsInCurrentBlock = 0

	// One commit per block: that is the store contract, so the benchmark must not batch.
	d.metrics.SetMainThreadPhase("committing")
	if err := d.db.CommitStateChanges(blockNum, changeSets); err != nil {
		return fmt.Errorf("failed to commit block %d: %w", blockNum, err)
	}
	d.nextBlockNumber++
	d.metrics.ReportDBCommit()
	d.reopenView()

	// Committing a block is not finishing it: the hash of a block committed a bounded number of
	// blocks ago is taken here, and waited for when hashing has fallen behind execution.
	if err := d.hashes.awaitBlock(); err != nil {
		return fmt.Errorf("failed to obtain a block hash after committing block %d: %w", blockNum, err)
	}

	d.metrics.SetMainThreadPhase("executing")

	return nil
}

// blockPairs returns the block's writes in the form the DB accepts, without the counter keys, which
// FinalizeBlock appends.
//
// There are two sources because there are two producers. A benchmark block arrives with its pairs
// already built by the block builder, so this is a field read and the conversion cost has already been
// paid off the critical path — the point of the whole arrangement. Setup has no block: it Puts account
// and contract data straight into pendingWrites, and there is nowhere earlier to have done the
// conversion, so it happens here. Setup runs once and is not what the benchmark reports.
func (d *Database) blockPairs() []*proto.KVPair {
	if d.currentBlock != nil {
		return d.currentBlock.Changeset()
	}

	pairs := make([]*proto.KVPair, 0, len(d.pendingWrites)+counterKeysPerBlock)
	for _, pair := range d.pendingWrites {
		pairs = append(pairs, pair)
	}
	d.pendingWrites = make(map[string]*proto.KVPair)
	return pairs
}

// reopenView replaces the read view with one over the block just committed. A view never observes
// writes made after it was opened, so without this every read would keep answering from the height
// the benchmark started at.
func (d *Database) reopenView() {
	d.view.Close()
	d.view = d.db.OpenView()
}

// Close the database and release any resources.
func (d *Database) Close(nextAccountID int64, nextErc20ContractID int64) error {
	fmt.Printf("Committing final batch.\n")

	// A failed final commit still has to release the stores below: they hold the state WAL directory's
	// exclusive lock, which an in-process retry needs back.
	var errs error
	if err := d.FinalizeBlock(nextAccountID, nextErc20ContractID); err != nil {
		errs = errors.Join(errs, fmt.Errorf("failed to commit batch: %w", err))
	}

	return errors.Join(errs, d.CloseWithoutFinalizing())
}

// Close the database and release any resources without finalizing the last batch.
func (d *Database) CloseWithoutFinalizing() error {
	fmt.Printf("Closing database.\n")

	var errs error

	// The collector prunes the stores closed below, so it stops before them. A failure to stop it
	// does not skip those closes: every failure here is collected and reported together.
	if err := d.garbageCollector.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("failed to close the storage garbage collector: %w", err))
	}

	// The view holds a reference into the store, which cannot release it while the view is open.
	d.view.Close()

	if err := d.db.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("failed to close database: %w", err))
	}

	return errs
}

// Set the function that flushes the executors. This setter is required to break a circular dependency.
func (d *Database) SetFlushFunc(flushFunc func()) {
	d.flushFunc = flushFunc
}
