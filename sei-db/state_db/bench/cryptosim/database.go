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

	// The current batch of key-value pairs waiting to be committed. Represents changes we are accumulating
	// as part of a simulated "block". Stored as value []byte; converted to NamedChangeSet when applied to the DB.
	batch *blockBatch

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
		batch:           newBlockBatch(),
		metrics:         metrics,
		nextBlockNumber: initialNextBlockNumber,
	}
}

// Insert a key-value pair into the database/cache.
//
// This method is safe to call concurrently with other calls to Put() and Get(). Is not thread
// safe with FinalizeBlock(). It is not thread safe to modify the returned value (make a copy first).
func (d *Database) Put(key []byte, value []byte) error {
	d.batch.Put(key, value)
	return nil
}

// Retrieve a value from the database/cache.
//
// This method is safe to call concurrently with other calls to Put() and Get(). Is not thread
// safe with FinalizeBlock().
func (d *Database) Get(key []byte) ([]byte, bool, error) {
	if value, found := d.batch.Get(key); found {
		return value, true, nil
	}

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
	d.transactionCount++
	d.transactionsInCurrentBlock++
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
	//
	// The pairs are carved out of one backing array rather than allocated individually, which is the
	// difference between one allocation per block and one per key. Indexing rather than appending to
	// the backing array is load-bearing: a regrow would move the structs out from under the pointers
	// already handed to pairs. The array is not reused across blocks, because ApplyChangeSets keeps
	// the changesets until the WAL write during Commit.
	count := d.batch.Len()
	pairs := make([]*proto.KVPair, 0, count+3)
	backing := make([]proto.KVPair, count)
	next := 0
	for key, value := range d.batch.Iterator() {
		backing[next] = proto.KVPair{Key: []byte(key), Value: value}
		pairs = append(pairs, &backing[next])
		next++
	}
	d.batch.Clear()

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
