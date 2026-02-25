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

	// The current batch of changesets waiting to be committed. Represents changes we are accumulating
	// as part of a simulated "block".
	batch *SyncMap[string, *proto.NamedChangeSet]
}

// Creates a new database for the cryptosim benchmark.
func NewDatabase(
	config *CryptoSimConfig,
	db wrappers.DBWrapper,
) *Database {
	return &Database{
		config: config,
		db:     db,
		batch:  NewSyncMap[string, *proto.NamedChangeSet](),
	}
}

// Insert a key-value pair into the database/cache.
//
// This method is safe to call concurrently with other calls to Put() and Get(). Is not thread
// safe with finalizeBlock().
func (d *Database) Put(key []byte, value []byte) error {
	stringKey := string(key)

	pending, found := d.batch.Get(stringKey)
	if found {
		pending.Changeset.Pairs[0].Value = value
		return nil
	}

	d.batch.Put(stringKey, &proto.NamedChangeSet{
		Name: wrappers.EVMStoreName,
		Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: key, Value: value},
		}},
	})

	return nil
}

// Retrieve a value from the database/cache.
//
// This method is safe to call concurrently with other calls to put() and get(). Is not thread
// safe with finalizeBlock().
func (d *Database) Get(key []byte) ([]byte, bool, error) {
	stringKey := string(key)

	pending, found := d.batch.Get(stringKey)
	if found {
		return pending.Changeset.Pairs[0].Value, true, nil
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
}

// Get the total number of transactions executed by the benchmark since it last started.
func (d *Database) TransactionCount() int64 {
	return d.transactionCount
}

// Commit the current batch if it has reached the configured number of transactions.
func (d *Database) MaybeFinalizeBlock(
	nextAccountID int64,
	nextErc20ContractID int64,
) error {
	if d.transactionsInCurrentBlock >= int64(d.config.TransactionsPerBlock) {
		return d.FinalizeBlock(nextAccountID, nextErc20ContractID, false)
	}
	return nil
}

// Push the current block out to the database.
func (d *Database) FinalizeBlock(
	nextAccountID int64,
	nextErc20ContractID int64,
	forceCommit bool,
) error {
	if d.transactionsInCurrentBlock == 0 {
		return nil
	}

	d.transactionsInCurrentBlock = 0

	changeSets := make([]*proto.NamedChangeSet, 0, d.transactionsInCurrentBlock+2)
	for _, cs := range d.batch.Iterator() {
		changeSets = append(changeSets, cs)
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

	err := d.db.ApplyChangeSets(changeSets)
	if err != nil {
		return fmt.Errorf("failed to apply change sets: %w", err)
	}

	// Persist the ERC20 contract ID counter in every batch.
	erc20ContractIDValue := make([]byte, 8)
	binary.BigEndian.PutUint64(erc20ContractIDValue, uint64(nextErc20ContractID))
	changeSets = append(changeSets, &proto.NamedChangeSet{
		Name: wrappers.EVMStoreName,
		Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: Erc20IDCounterKey(), Value: erc20ContractIDValue},
		}},
	})

	// Periodically commit the changes to the database.
	d.uncommittedBlockCount++
	if forceCommit || d.uncommittedBlockCount >= int64(d.config.BlocksPerCommit) {
		_, err := d.db.Commit()
		if err != nil {
			return fmt.Errorf("failed to commit: %w", err)
		}
		d.uncommittedBlockCount = 0
	}

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
