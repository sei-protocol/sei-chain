package cryptosim

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

// The test runner for the cryptosim benchmark.
type CryptoSim struct {
	ctx context.Context

	// The configuration for the benchmark.
	config *CryptoSimConfig

	// The database implementation to use for the benchmark.
	db wrappers.DBWrapper

	// The source of randomness for the benchmark.
	rand *RandomBuffer

	// The next account ID to be used when creating a new account.
	nextAccountID int64

	// The current batch of changesets waiting to be committed.
	batch map[string]*proto.NamedChangeSet

	// Memiavl nonce key for the account ID counter (0x0a + reserved 20-byte addr).
	// Uses non-zero sentinel address to avoid potential edge cases with all-zero key.
	accountIDCounterKey []byte
}

// Creates a new cryptosim benchmark runner.
func NewCryptoSim(
	ctx context.Context,
	config *CryptoSimConfig,
) (*CryptoSim, error) {

	if config.DataDir == "" {
		return nil, fmt.Errorf("data directory is required")
	}

	dataDir, err := resolveAndCreateDataDir(config.DataDir)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Running cryptosim benchmark from data directory: %s\n", dataDir)

	db, err := wrappers.NewDBImpl(config.Backend, dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	fmt.Printf("Initializing random buffer\n")
	rand := NewRandomBuffer(config.RandomBufferSize, config.Seed)

	// Reserved address for counter: 20 bytes of 0x01 (avoids all-zero key edge cases).
	reservedAddr := make([]byte, 20)
	for i := range reservedAddr {
		reservedAddr[i] = 0x01
	}
	c := &CryptoSim{
		ctx:                 ctx,
		config:              config,
		db:                  db,
		rand:                rand,
		batch:               make(map[string]*proto.NamedChangeSet, config.TransactionsPerBlock),
		accountIDCounterKey: evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, reservedAddr),
	}

	err = c.setup()
	if err != nil {
		return nil, fmt.Errorf("failed to setup benchmark: %w", err)
	}

	c.start()
	return c, nil
}

func (c *CryptoSim) setup() error {

	// Ensure that we at least have as many accounts as the hot set + 1. This simplifies logic elsewhere.
	if c.config.MinimumNumberOfAccounts < c.config.HotSetSize+1 {
		c.config.MinimumNumberOfAccounts = c.config.HotSetSize + 1
	}

	nextAccountID, found, err := c.db.Read(c.accountIDCounterKey)
	if err != nil {
		return fmt.Errorf("failed to read account counter: %w", err)
	}
	if found {
		c.nextAccountID = int64(binary.BigEndian.Uint64(nextAccountID))
	}

	fmt.Printf("There are currently %d keys in the database.\n", c.nextAccountID)

	if c.nextAccountID >= int64(c.config.MinimumNumberOfAccounts) {
		return nil
	}

	fmt.Printf("Benchmark is configured to run with a minimum of %d accounts. Creating %d new accounts.\n",
		c.config.MinimumNumberOfAccounts, int64(c.config.MinimumNumberOfAccounts)-c.nextAccountID)

	for c.nextAccountID < int64(c.config.MinimumNumberOfAccounts) {
		err := c.createNewAccount()
		if err != nil {
			return fmt.Errorf("failed to create new account: %w", err)
		}
		err = c.maybeCommitBatch()
		if err != nil {
			return fmt.Errorf("failed to maybe commit batch: %w", err)
		}
	}
	if err := c.commitBatch(); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}
	if _, err := c.db.Commit(); err != nil {
		return fmt.Errorf("failed to commit database: %w", err)
	}

	fmt.Printf("There are now %d accounts in the database.\n", c.nextAccountID)

	return nil
}

func (c *CryptoSim) start() {
	// TODO
}

// Creates a new account and writes it to the database.
func (c *CryptoSim) createNewAccount() error {

	accountID := c.nextAccountID
	c.nextAccountID++

	// Use memiavl code key format (0x07 + addr) so FlatKV persists account data.
	addr := c.rand.Address(c.config.AccountKeySize, accountID)
	address := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr)
	balance := c.rand.Int64()

	accountData := make([]byte, c.config.PaddedAccountSize)

	// The first 8 bytes of the account data are the balance. For the sake of simplicity,
	// For the sake of simplicity, we allow negative balances and don't care about overflow.
	binary.BigEndian.PutUint64(accountData[:8], uint64(balance))

	// The remaining bytes are random data for padding.
	randomBytes := c.rand.Bytes(c.config.PaddedAccountSize - 8)
	copy(accountData[8:], randomBytes)

	err := c.put(address, accountData)
	if err != nil {
		return fmt.Errorf("failed to put account: %w", err)
	}

	return nil
}

// Commit the current batch if it has reached the configured number of transactions.
func (c *CryptoSim) maybeCommitBatch() error {
	if len(c.batch) >= c.config.TransactionsPerBlock {
		return c.commitBatch()
	}
	return nil
}

// Commit the current batch.
func (c *CryptoSim) commitBatch() error {
	if len(c.batch) == 0 {
		return nil
	}

	changeSets := make([]*proto.NamedChangeSet, 0, len(c.batch)+1)
	for _, cs := range c.batch {
		changeSets = append(changeSets, cs)
	}
	// Persist the account ID counter in every batch.
	nonceValue := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceValue, uint64(c.nextAccountID))
	changeSets = append(changeSets, &proto.NamedChangeSet{
		Name: wrappers.EVMStoreName,
		Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: c.accountIDCounterKey, Value: nonceValue},
		}},
	})

	err := c.db.ApplyChangeSets(changeSets)
	if err != nil {
		return fmt.Errorf("failed to apply change sets: %w", err)
	}
	c.batch = make(map[string]*proto.NamedChangeSet)
	return nil
}

// Insert a key-value pair into the database/cache.
func (c *CryptoSim) put(key []byte, value []byte) error {
	stringKey := string(key)

	pending, found := c.batch[stringKey]
	if found {
		pending.Changeset.Pairs[0].Value = value
		return nil
	}

	c.batch[stringKey] = &proto.NamedChangeSet{
		Name: wrappers.EVMStoreName,
		Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: key, Value: value},
		}},
	}

	return nil
}

// Retrieve a value from the database/cache.
func (c *CryptoSim) get(key []byte) ([]byte, bool, error) {
	stringKey := string(key)

	pending, found := c.batch[stringKey]
	if found {
		return pending.Changeset.Pairs[0].Value, true, nil
	}

	value, found, err := c.db.Read(key)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read from database: %w", err)
	}
	if found {
		return value, true, nil
	}

	return nil, false, nil
}

// Shut down the benchmark and release any resources.
func (c *CryptoSim) Close() error {

	fmt.Printf("Committing final batch...\n")

	if err := c.commitBatch(); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}
	if _, err := c.db.Commit(); err != nil {
		return fmt.Errorf("failed to commit database: %w", err)
	}

	fmt.Printf("Closing database...\n")
	err := c.db.Close()
	if err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	fmt.Printf("benchmark terminated successfully\n")

	// Specifically release rand, since it's likely to hold a lot of memory.
	c.rand = nil

	return nil
}
