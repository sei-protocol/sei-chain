package cryptosim

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

// The test runner for the cryptosim benchmark.
type CryptoSim struct {
	ctx    context.Context
	cancel context.CancelFunc

	// The configuration for the benchmark.
	config *CryptoSimConfig

	// The database implementation to use for the benchmark.
	db wrappers.DBWrapper

	// The source of randomness for the benchmark.
	rand *CannedRandom

	// The next account ID to be used when creating a new account.
	nextAccountID int64

	// The total number of transactions executed by the benchmark since it last started.
	transactionCount int64

	// The number of blocks that have been executed since the last commit.
	uncommitedBlockCount int64

	// The current batch of changesets waiting to be committed. Represents changes we are accumulating
	// as part of a simulated "block".
	batch map[string]*proto.NamedChangeSet

	// Memiavl nonce key for the account ID counter (0x0a + reserved 20-byte addr).
	// Uses non-zero sentinel address to avoid potential edge cases with all-zero key.
	accountIDCounterKey []byte

	// The address of the fee account (i.e. the account that collects gas fees). This is a special account
	// and has account ID 0. Since we reuse this account very often, it is cached for performance.
	feeCollectionAddress []byte

	// If this much time has passed since the last console update, the benchmark will print a report to the console.
	consoleUpdatePeriod time.Duration

	// The time of the last console update.
	lastConsoleUpdateTime time.Time

	// The number of transactions executed by the benchmark the last time the console was updated.
	lastConsoleUpdateTransactionCount int64

	// The time the benchmark started.
	startTimestamp time.Time

	// The run channel sends a signal on this channel when it has halted.
	runHaltedChan chan struct{}
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

	fmt.Printf("running cryptosim benchmark from data directory: %s\n", dataDir)

	db, err := wrappers.NewDBImpl(config.Backend, dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	fmt.Printf("initializing random number generator\n")
	rand := NewCannedRandom(config.CannedRandomSize, config.Seed)

	feeCollectionAddress := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, rand.Address(config.AccountKeySize, 0))

	// Reserved address for counter: 20 bytes of 0x01 (avoids all-zero key edge cases).
	reservedAddr := make([]byte, 20)
	for i := range reservedAddr {
		reservedAddr[i] = 0x01
	}

	consoleUpdatePeriod := time.Duration(config.ConsoleUpdateIntervalSeconds * float64(time.Second))

	start := time.Now()

	ctx, cancel := context.WithCancel(ctx)

	c := &CryptoSim{
		ctx:                               ctx,
		cancel:                            cancel,
		config:                            config,
		db:                                db,
		rand:                              rand,
		batch:                             make(map[string]*proto.NamedChangeSet, config.TransactionsPerBlock),
		accountIDCounterKey:               evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, reservedAddr),
		feeCollectionAddress:              feeCollectionAddress,
		consoleUpdatePeriod:               consoleUpdatePeriod,
		lastConsoleUpdateTime:             start,
		lastConsoleUpdateTransactionCount: 0,
		runHaltedChan:                     make(chan struct{}, 1),
	}

	err = c.setup()
	if err != nil {
		return nil, fmt.Errorf("failed to setup benchmark: %w", err)
	}

	c.startTimestamp = time.Now()

	go c.run()
	return c, nil
}

// Prepare the benchmark by pre-populating the database with the minimum number of accounts.
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

	fmt.Printf("There are currently %s keys in the database.\n", int64Commas(c.nextAccountID))

	if c.nextAccountID >= int64(c.config.MinimumNumberOfAccounts) {
		return nil
	}

	fmt.Printf("Benchmark is configured to run with a minimum of %s accounts. Creating %s new accounts.\n",
		int64Commas(int64(c.config.MinimumNumberOfAccounts)),
		int64Commas(int64(c.config.MinimumNumberOfAccounts)-c.nextAccountID))

	for c.nextAccountID < int64(c.config.MinimumNumberOfAccounts) {

		if c.ctx.Err() != nil {
			fmt.Printf("benchmark aborted during account creation\n")
			break
		}

		_, err := c.createNewAccount()
		if err != nil {
			return fmt.Errorf("failed to create new account: %w", err)
		}
		err = c.maybeFinalizeBlock()
		if err != nil {
			return fmt.Errorf("failed to maybe commit batch: %w", err)
		}

		if c.nextAccountID%c.config.SetupUpdateIntervalCount == 0 {
			fmt.Printf("created %s of %s accounts\r",
				int64Commas(c.nextAccountID), int64Commas(int64(c.config.MinimumNumberOfAccounts)))
		}
	}
	fmt.Printf("created %d of %d accounts\n", c.nextAccountID, c.config.MinimumNumberOfAccounts)
	if err := c.finalizeBlock(); err != nil {
		return fmt.Errorf("failed to commit block: %w", err)
	}
	if _, err := c.db.Commit(); err != nil {
		return fmt.Errorf("failed to commit database: %w", err)
	}
	c.uncommitedBlockCount = 0

	fmt.Printf("There are now %d accounts in the database.\n", c.nextAccountID)

	return nil
}

// The main loop of the benchmark.
func (c *CryptoSim) run() {

	defer func() {
		c.runHaltedChan <- struct{}{}
	}()

	for {
		select {
		case <-c.ctx.Done():
			if c.transactionCount > 0 {
				c.generateConsoleReport(true)
				fmt.Printf("\ntransaction workload halted\n")
			}
			return
		default:
			err := c.executeTransaction()
			if err != nil {
				fmt.Printf("failed to execute transaction: %v\n", err)
			}
			c.transactionCount++
			c.generateConsoleReport(false)
		}
	}
}

// Perform a single transaction.
func (c *CryptoSim) executeTransaction() error {

	// Determine which accounts will be involved in the transaction.
	srcAccount, err := c.randomAccount()
	if err != nil {
		return fmt.Errorf("failed to select source account: %w", err)
	}
	dstAccount, err := c.randomAccount()
	if err != nil {
		return fmt.Errorf("failed to select destination account: %w", err)
	}

	// Read the current balances of the accounts.
	// TODO we should be able to parrelize these reads, do this as a follow up
	srcValue, found, err := c.get(srcAccount)
	if err != nil {
		return fmt.Errorf("failed to get source account: %w", err)
	}
	if !found {
		return fmt.Errorf("source account not found")
	}

	dstValue, found, err := c.get(dstAccount)
	if err != nil {
		return fmt.Errorf("failed to get destination account: %w", err)
	}
	if !found {
		return fmt.Errorf("destination account not found")
	}

	feeValue, found, err := c.get(c.feeCollectionAddress)
	if err != nil {
		return fmt.Errorf("failed to get fee collection account: %w", err)
	}
	if !found {
		return fmt.Errorf("fee collection account not found")
	}

	// Generate new balances for the accounts.
	// The "balance" is simulated as the first 8 bytes of the account data.
	// We can just choose a new random balance, since we don't care about the actual balance.

	newSrcBalance := c.rand.Int64()
	newDstBalance := c.rand.Int64()
	newFeeBalance := c.rand.Int64()

	binary.BigEndian.PutUint64(srcValue[:8], uint64(newSrcBalance))
	binary.BigEndian.PutUint64(dstValue[:8], uint64(newDstBalance))
	binary.BigEndian.PutUint64(feeValue[:8], uint64(newFeeBalance))

	// Write the new balances to the DB.
	err = c.put(srcAccount, srcValue)
	if err != nil {
		return fmt.Errorf("failed to put source account: %w", err)
	}
	err = c.put(dstAccount, dstValue)
	if err != nil {
		return fmt.Errorf("failed to put destination account: %w", err)
	}
	err = c.put(c.feeCollectionAddress, feeValue)
	if err != nil {
		return fmt.Errorf("failed to put fee collection account: %w", err)
	}

	return nil
}

// Generates a human readable report of the benchmark's progress.
func (c *CryptoSim) generateConsoleReport(force bool) {

	// TODO measuring time each cycle is not efficient

	now := time.Now()
	timeSinceLastUpdate := now.Sub(c.lastConsoleUpdateTime)
	transactionsSinceLastUpdate := c.transactionCount - c.lastConsoleUpdateTransactionCount

	if !force &&
		timeSinceLastUpdate < c.consoleUpdatePeriod &&
		transactionsSinceLastUpdate < int64(c.config.ConsoleUpdateIntervalTransactions) {

		// Not yet time to update the console.
		return
	}

	c.lastConsoleUpdateTime = now
	c.lastConsoleUpdateTransactionCount = c.transactionCount

	totalElapsedTime := now.Sub(c.startTimestamp)
	transactionsPerSecond := float64(c.transactionCount) / totalElapsedTime.Seconds()

	// Generate the report.
	fmt.Printf("%s txns executed in %v (%s txns/sec), total number of accounts: %s\r",
		int64Commas(c.transactionCount),
		totalElapsedTime,
		formatNumberFloat64(transactionsPerSecond, 2),
		int64Commas(c.nextAccountID))
}

// Select a random account for a transaction.
func (c *CryptoSim) randomAccount() ([]byte, error) {
	hot := c.rand.Float64() < c.config.HotAccountProbably

	if hot {
		firstHotAccountID := 1
		lastHotAccountID := c.config.HotSetSize
		accountID := c.rand.Int64Range(int64(firstHotAccountID), int64(lastHotAccountID+1))
		addr := c.rand.Address(c.config.AccountKeySize, accountID)
		return evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr), nil
	} else {

		new := c.rand.Float64() < c.config.NewAccountProbably
		if new {
			account, err := c.createNewAccount()
			if err != nil {
				return nil, fmt.Errorf("failed to create new account: %w", err)
			}
			return account, nil
		}

		firstNonHotAccountID := c.config.HotSetSize + 1
		accountID := c.rand.Int64Range(int64(firstNonHotAccountID), int64(c.nextAccountID))
		addr := c.rand.Address(c.config.AccountKeySize, accountID)
		return evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr), nil
	}
}

// Creates a new account and writes it to the database. Returns the address of the new account.
func (c *CryptoSim) createNewAccount() ([]byte, error) {

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
		return nil, fmt.Errorf("failed to put account: %w", err)
	}

	return address, nil
}

// Commit the current batch if it has reached the configured number of transactions.
func (c *CryptoSim) maybeFinalizeBlock() error {
	if len(c.batch) >= c.config.TransactionsPerBlock {
		return c.finalizeBlock()
	}
	return nil
}

// Push the current block out to the database.
func (c *CryptoSim) finalizeBlock() error {
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

	// Periodically commit the changes to the database.
	c.uncommitedBlockCount++
	if c.uncommitedBlockCount >= int64(c.config.BlocksPerCommit) {
		_, err := c.db.Commit()
		if err != nil {
			return fmt.Errorf("failed to commit: %w", err)
		}
		c.uncommitedBlockCount = 0
	}

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
	c.cancel()
	<-c.runHaltedChan

	fmt.Printf("committing final batch\n")

	if err := c.finalizeBlock(); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}
	if _, err := c.db.Commit(); err != nil {
		return fmt.Errorf("failed to commit database: %w", err)
	}

	fmt.Printf("closing database\n")
	err := c.db.Close()
	if err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	fmt.Printf("benchmark terminated successfully\n")

	// Specifically release rand, since it's likely to hold a lot of memory.
	c.rand = nil

	return nil
}
