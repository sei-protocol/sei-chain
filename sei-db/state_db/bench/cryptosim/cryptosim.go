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

const (
	// Used to store the next account ID to be used when creating a new account.
	nonceKey = string(evm.EVMKeyNonce) + "nonce"
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
	batch []*proto.NamedChangeSet
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

	c := &CryptoSim{
		ctx:    ctx,
		config: config,
		db:     db,
		rand:   rand,
		batch:  make([]*proto.NamedChangeSet, 0, config.TransactionsPerBlock),
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

	nextAccountID, found, err := c.db.Read([]byte(nonceKey))
	if err != nil {
		return fmt.Errorf("failed to read nonce: %w", err)
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
	}
	c.commitBatch()

	return nil
}

func (c *CryptoSim) start() {
	// TODO
}

// Creates a new account and writes it to the database.
func (c *CryptoSim) createNewAccount() error {

	accountID := c.nextAccountID
	c.nextAccountID++

	address := c.rand.Address(nonceKey, c.config.AccountKeySize, accountID)
	balance := c.rand.Int64()

	accountData := make([]byte, c.config.PaddedAccountSize)

	// The first 8 bytes of the account data are the balance. For the sake of simplicity,
	// For the sake of simplicity, we allow negative balances and don't care about overflow.
	binary.BigEndian.PutUint64(accountData[:8], uint64(balance))

	// The remaining bytes are random data for padding.
	randomBytes := c.rand.Bytes(c.config.PaddedAccountSize - 8)
	copy(accountData[8:], randomBytes)

	cs := &proto.NamedChangeSet{
		Name: wrappers.EVMStoreName,
		Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: address, Value: accountData},
		}},
	}
	c.batch = append(c.batch, cs)

	c.maybeCommitBatch()

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
	err := c.db.ApplyChangeSets(c.batch)
	if err != nil {
		return fmt.Errorf("failed to apply change sets: %w", err)
	}
	c.batch = make([]*proto.NamedChangeSet, 0, c.config.TransactionsPerBlock)
	return nil
}
