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

	// The current batch of key-value pairs waiting to be committed. Represents changes we are accumulating
	// as part of a simulated "block". Stored as value []byte; converted to NamedChangeSet when applied to the DB.
	batch *SyncMap[string, []byte]

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
		batch:            NewSyncMap[string, []byte](),
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

// Insert a key-value pair into the database/cache.
//
// This method is safe to call concurrently with other calls to Put() and Get(). Is not thread
// safe with FinalizeBlock(). It is not thread safe to modify the returned value (make a copy first).
func (d *Database) Put(key []byte, value []byte) error {
	d.batch.Put(string(key), value)
	return nil
}

// Retrieve a value from the database/cache.
//
// This method is safe to call concurrently with other calls to Put() and Get(). Is not thread
// safe with FinalizeBlock().
func (d *Database) Get(key []byte) ([]byte, bool) {
	if value, found := d.batch.Get(string(key)); found {
		return value, true
	}
	return d.view.Get(keys.EVMStoreKey, key)
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

	changeSets := make([]*proto.NamedChangeSet, 0, d.transactionsInCurrentBlock+3)
	for key, value := range d.batch.Iterator() {
		changeSets = append(changeSets, &proto.NamedChangeSet{
			Name:      keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte(key), Value: value}}},
		})
	}
	d.batch.Clear()

	// Persist the account ID counter in every batch.
	nonceValue := make([]byte, 8)
	//nolint:gosec // G115 - nextAccountID is benchmark counter, overflow acceptable
	binary.BigEndian.PutUint64(nonceValue, uint64(nextAccountID))
	changeSets = append(changeSets, &proto.NamedChangeSet{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: AccountIDCounterKey(), Value: nonceValue},
		}},
	})

	// Persist the ERC20 contract ID counter in every batch.
	erc20ContractIDValue := make([]byte, 8)
	//nolint:gosec // G115 - nextErc20ContractID is benchmark counter, overflow acceptable
	binary.BigEndian.PutUint64(erc20ContractIDValue, uint64(nextErc20ContractID))
	changeSets = append(changeSets, &proto.NamedChangeSet{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: Erc20IDCounterKey(), Value: erc20ContractIDValue},
		}},
	})

	// Persist the block number counter in every batch.
	blockNum := d.nextBlockNumber
	blockNumberValue := make([]byte, 8)
	//nolint:gosec // G115 - blockNum is a benchmark counter, overflow acceptable
	binary.BigEndian.PutUint64(blockNumberValue, uint64(blockNum))
	changeSets = append(changeSets, &proto.NamedChangeSet{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: BlockNumberCounterKey(), Value: blockNumberValue},
		}},
	})

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

	if err := d.FinalizeBlock(nextAccountID, nextErc20ContractID); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}

	return d.CloseWithoutFinalizing()
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
