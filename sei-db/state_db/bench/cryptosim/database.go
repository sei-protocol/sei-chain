package cryptosim

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
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

	// The number of blocks that have been executed since the last commit.
	uncommittedBlockCount int64

	// The current batch of key-value pairs waiting to be committed. Represents changes we are accumulating
	// as part of a simulated "block". Stored as value []byte; converted to NamedChangeSet when applied to the DB.
	batch *SyncMap[string, []byte]

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
) *Database {
	return &Database{
		config:  config,
		db:      db,
		batch:   NewSyncMap[string, []byte](),
		metrics: metrics,
	}
}

// Insert a key-value pair into the database/cache.
//
// This method is safe to call concurrently with other calls to Put() and Get(). Is not thread
// safe with FinalizeBlock().
func (d *Database) Put(key []byte, value []byte) error {
	d.batch.Put(string(key), value)
	return nil
}

// Retrieve a value from the database/cache.
//
// This method is safe to call concurrently with other calls to Put() and Get(). Is not thread
// safe with FinalizeBlock().
func (d *Database) Get(key []byte) ([]byte, bool, error) {
	if value, found := d.batch.Get(string(key)); found {
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

// Signal that a transaction has been executed.
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
		err := d.FinalizeBlock(nextAccountID, nextErc20ContractID, false)
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
	forceCommit bool,
) error {

	// Wait for all transactions in the current block to be executed.
	if d.flushFunc != nil {
		d.flushFunc()
	}

	if d.transactionsInCurrentBlock == 0 {
		return nil
	}

	d.metrics.SetMainThreadPhase("finalizing")

	changeSets := make([]*proto.NamedChangeSet, 0, d.transactionsInCurrentBlock+2)
	for key, value := range d.batch.Iterator() {
		changeSets = append(changeSets, &proto.NamedChangeSet{
			Name:      wrappers.EVMStoreName,
			Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{{Key: []byte(key), Value: value}}},
		})
	}
	d.batch.Clear()

	// Persist the account ID counter in every batch.
	nonceValue := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceValue, uint64(nextAccountID))
	changeSets = append(changeSets, &proto.NamedChangeSet{
		Name: wrappers.EVMStoreName,
		Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: AccountIDCounterKey(), Value: nonceValue},
		}},
	})

	// Persist the ERC20 contract ID counter in every batch.
	erc20ContractIDValue := make([]byte, 8)
	binary.BigEndian.PutUint64(erc20ContractIDValue, uint64(nextErc20ContractID))
	changeSets = append(changeSets, &proto.NamedChangeSet{
		Name: wrappers.EVMStoreName,
		Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: Erc20IDCounterKey(), Value: erc20ContractIDValue},
		}},
	})

	err := d.db.ApplyChangeSets(changeSets)
	if err != nil {
		return fmt.Errorf("failed to apply change sets: %w", err)
	}

	d.metrics.ReportBlockFinalized(d.transactionsInCurrentBlock)
	d.transactionsInCurrentBlock = 0

	// Periodically commit the changes to the database.
	d.uncommittedBlockCount++
	if forceCommit || d.uncommittedBlockCount >= int64(d.config.BlocksPerCommit) {
		d.metrics.SetMainThreadPhase("committing")
		_, err := d.db.Commit()
		if err != nil {
			return fmt.Errorf("failed to commit: %w", err)
		}
		d.metrics.ReportDBCommit()
		d.uncommittedBlockCount = 0
	}

	d.metrics.SetMainThreadPhase("executing")

	return nil
}

// Close the database and release any resources.
func (d *Database) Close(nextAccountID int64, nextErc20ContractID int64) error {
	fmt.Printf("Committing final batch.\n")

	if err := d.FinalizeBlock(nextAccountID, nextErc20ContractID, true); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}

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
