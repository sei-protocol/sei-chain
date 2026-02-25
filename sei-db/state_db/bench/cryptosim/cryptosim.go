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

const (
	// Used to store the next account ID in the database.
	accountIdCounterKey = "accountIdCounterKey"
	// Used to store the next ERC20 contract ID in the database.
	erc20IdCounterKey = "erc20IdCounterKey"

	accountPrefix    = 'a'
	contractPrefix   = 'c'
	ethStoragePrefix = 's'
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

	// Key for the account ID counter in the database.
	accountIDCounterKey []byte

	// The next ERC20 contract ID to be used when creating a new ERC20 contract.
	nextErc20ContractID int64

	// Key for the ERC20 contract ID counter in the database.
	erc20IDCounterKey []byte

	// The total number of transactions executed by the benchmark since it last started.
	transactionCount int64

	// The number of blocks that have been executed since the last commit.
	uncommittedBlockCount int64

	// The current batch of changesets waiting to be committed. Represents changes we are accumulating
	// as part of a simulated "block".
	batch *SyncMap[string, *proto.NamedChangeSet]

	// A count of the number of transactions in the current batch.
	transactionsInCurrentBlock int64

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

	fmt.Printf("Running cryptosim benchmark from data directory: %s\n", dataDir)

	db, err := wrappers.NewDBImpl(config.Backend, dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	fmt.Printf("Initializing random number generator.\n")
	rand := NewCannedRandom(config.CannedRandomSize, config.Seed)

	feeCollectionAddress := evm.BuildMemIAVLEVMKey(
		evm.EVMKeyCode,
		rand.Address(accountPrefix, 0),
	)

	accountIdCounterBytes := make([]byte, 20)
	copy(accountIdCounterBytes, []byte(accountIdCounterKey))
	accountIDCounterKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, accountIdCounterBytes)

	erc20IdCounterBytes := make([]byte, 20)
	copy(erc20IdCounterBytes, []byte(erc20IdCounterKey))
	erc20IDCounterKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, erc20IdCounterBytes)

	consoleUpdatePeriod := time.Duration(config.ConsoleUpdateIntervalSeconds * float64(time.Second))

	start := time.Now()

	ctx, cancel := context.WithCancel(ctx)

	c := &CryptoSim{
		ctx:                               ctx,
		cancel:                            cancel,
		config:                            config,
		db:                                db,
		rand:                              rand,
		batch:                             NewSyncMap[string, *proto.NamedChangeSet](),
		accountIDCounterKey:               accountIDCounterKey,
		erc20IDCounterKey:                 erc20IDCounterKey,
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
	err := c.setupAccounts()
	if err != nil {
		return fmt.Errorf("failed to setup accounts: %w", err)
	}
	err = c.setupErc20Contracts()
	if err != nil {
		return fmt.Errorf("failed to setup ERC20 contracts: %w", err)
	}
	return nil
}

func (c *CryptoSim) setupAccounts() error {
	// Ensure that we at least have as many accounts as the hot set + 2. This simplifies logic elsewhere.
	if c.config.MinimumNumberOfAccounts < c.config.HotAccountSetSize+2 {
		c.config.MinimumNumberOfAccounts = c.config.HotAccountSetSize + 2
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

		_, _, err := c.createNewAccount(true)
		if err != nil {
			return fmt.Errorf("failed to create new account: %w", err)
		}
		c.transactionsInCurrentBlock++
		err = c.maybeFinalizeBlock()
		if err != nil {
			return fmt.Errorf("failed to maybe commit batch: %w", err)
		}

		if c.nextAccountID%c.config.SetupUpdateIntervalCount == 0 {
			fmt.Printf("Created %s of %s accounts.\r",
				int64Commas(c.nextAccountID), int64Commas(int64(c.config.MinimumNumberOfAccounts)))
		}
	}
	if c.nextAccountID >= c.config.SetupUpdateIntervalCount {
		fmt.Printf("\n")
	}
	fmt.Printf("Created %s of %s accounts.\n",
		int64Commas(c.nextAccountID), int64Commas(int64(c.config.MinimumNumberOfAccounts)))
	if err := c.finalizeBlock(); err != nil {
		return fmt.Errorf("failed to commit block: %w", err)
	}
	if _, err := c.db.Commit(); err != nil {
		return fmt.Errorf("failed to commit database: %w", err)
	}
	c.uncommittedBlockCount = 0

	fmt.Printf("There are now %d accounts in the database.\n", c.nextAccountID)

	return nil
}

func (c *CryptoSim) setupErc20Contracts() error {

	// Ensure that we at least have as many ERC20 contracts as the hot set + 1. This simplifies logic elsewhere.
	if c.config.MinimumNumberOfErc20Contracts < c.config.HotErc20ContractSetSize+1 {
		c.config.MinimumNumberOfErc20Contracts = c.config.HotErc20ContractSetSize + 1
	}

	nextErc20ContractID, found, err := c.db.Read(c.erc20IDCounterKey)
	if err != nil {
		return fmt.Errorf("failed to read ERC20 contract counter: %w", err)
	}
	if found {
		c.nextErc20ContractID = int64(binary.BigEndian.Uint64(nextErc20ContractID))
	}

	fmt.Printf("There are currently %s simulated ERC20 contracts in the database.\n",
		int64Commas(c.nextErc20ContractID))

	if c.nextErc20ContractID >= int64(c.config.MinimumNumberOfErc20Contracts) {
		return nil
	}

	fmt.Printf("Benchmark is configured to run with a minimum of %s simulated ERC20 contracts. "+
		"Creating %s new ERC20 contracts.\n",
		int64Commas(int64(c.config.MinimumNumberOfErc20Contracts)),
		int64Commas(int64(c.config.MinimumNumberOfErc20Contracts)-c.nextErc20ContractID))

	for c.nextErc20ContractID < int64(c.config.MinimumNumberOfErc20Contracts) {
		if c.ctx.Err() != nil {
			fmt.Printf("benchmark aborted during ERC20 contract creation\n")
			break
		}

		c.transactionsInCurrentBlock++

		_, err := c.createNewErc20Contract()
		if err != nil {
			return fmt.Errorf("failed to create new ERC20 contract: %w", err)
		}
		err = c.maybeFinalizeBlock()
		if err != nil {
			return fmt.Errorf("failed to maybe commit batch: %w", err)
		}

		if c.nextErc20ContractID%c.config.SetupUpdateIntervalCount == 0 {
			fmt.Printf("Created %s of %s simulated ERC20 contracts.\r",
				int64Commas(c.nextErc20ContractID), int64Commas(int64(c.config.MinimumNumberOfErc20Contracts)))
		}
	}

	// As a final step, write the ERC20 contract ID counter to the database.
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, uint64(c.nextErc20ContractID))
	err = c.put(c.erc20IDCounterKey, data)
	if err != nil {
		return fmt.Errorf("failed to put ERC20 contract ID counter: %w", err)
	}

	if c.nextErc20ContractID >= c.config.SetupUpdateIntervalCount {
		fmt.Printf("\n")
	}
	fmt.Printf("Created %s of %s simulated ERC20 contracts.\n",
		int64Commas(c.nextErc20ContractID), int64Commas(int64(c.config.MinimumNumberOfErc20Contracts)))
	if err := c.finalizeBlock(); err != nil {
		return fmt.Errorf("failed to commit block: %w", err)
	}
	if _, err := c.db.Commit(); err != nil {
		return fmt.Errorf("failed to commit database: %w", err)
	}
	c.uncommittedBlockCount = 0

	fmt.Printf("There are now %d simulated ERC20 contracts in the database.\n", c.nextErc20ContractID)

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
				fmt.Printf("\nTransaction workload halted.\n")
			}
			return
		default:

			txn, err := BuildTransaction(c)
			if err != nil {
				fmt.Printf("\nfailed to build transaction: %v\n", err)
				continue
			}

			err = txn.Execute(c)
			if err != nil {
				fmt.Printf("\nfailed to execute transaction: %v\n", err)
				continue
			}

			err = c.maybeFinalizeBlock()
			if err != nil {
				fmt.Printf("error finalizing block: %v\n", err)
				continue
			}

			c.transactionCount++
			c.transactionsInCurrentBlock++
			c.generateConsoleReport(false)
		}
	}
}

// Generates a human readable report of the benchmark's progress.
func (c *CryptoSim) generateConsoleReport(force bool) {

	// Future work: determine overhead of measuring time each cycle and change accordingly.

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
func (c *CryptoSim) randomAccount() (id int64, address []byte, isNew bool, err error) {
	hot := c.rand.Float64() < c.config.HotAccountProbability

	if hot {
		firstHotAccountID := 1
		lastHotAccountID := c.config.HotAccountSetSize
		accountID := c.rand.Int64Range(int64(firstHotAccountID), int64(lastHotAccountID+1))
		addr := c.rand.Address(accountPrefix, accountID)
		return accountID, evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr), false, nil
	} else {

		new := c.rand.Float64() < c.config.NewAccountProbability
		if new {
			id, address, err := c.createNewAccount(false)
			if err != nil {
				return 0, nil, false, fmt.Errorf("failed to create new account: %w", err)
			}
			return id, address, true, nil
		}

		firstNonHotAccountID := c.config.HotAccountSetSize + 1
		accountID := c.rand.Int64Range(int64(firstNonHotAccountID), int64(c.nextAccountID))
		addr := c.rand.Address(accountPrefix, accountID)
		return accountID, evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr), false, nil
	}
}

// Selects a random account slot for a transaction.
func (c *CryptoSim) randomAccountSlot(accountID int64) ([]byte, error) {
	slotNumber := c.rand.Int64Range(0, int64(c.config.Erc20InteractionsPerAccount))
	slotID := accountID*int64(c.config.Erc20InteractionsPerAccount) + slotNumber

	addr := c.rand.Address(ethStoragePrefix, slotID)
	return evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr), nil
}

// Selects a random ERC20 contract for a transaction.
func (c *CryptoSim) randomErc20Contract() ([]byte, error) {

	hot := c.rand.Float64() < c.config.HotErc20ContractProbability

	if hot {
		erc20ContractID := c.rand.Int64Range(0, int64(c.config.HotErc20ContractSetSize))
		addr := c.rand.Address(contractPrefix, erc20ContractID)
		return evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr), nil
	}

	// Otherwise, select a cold ERC20 contract at random.

	erc20ContractID := c.rand.Int64Range(int64(c.config.HotErc20ContractSetSize), int64(c.nextErc20ContractID))
	addr := c.rand.Address(contractPrefix, erc20ContractID)
	return evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr), nil
}

// Creates a new account and optinally writes it to the database. Returns the address of the new account.
func (c *CryptoSim) createNewAccount(write bool) (id int64, address []byte, err error) {

	accountID := c.nextAccountID
	c.nextAccountID++

	// Use memiavl code key format (0x07 + addr) so FlatKV persists account data.
	addr := c.rand.Address(accountPrefix, accountID)
	address = evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr)

	if !write {
		return accountID, address, nil
	}

	balance := c.rand.Int64()

	accountData := make([]byte, c.config.PaddedAccountSize)

	binary.BigEndian.PutUint64(accountData[:8], uint64(balance))

	// The remaining bytes are random data for padding.
	randomBytes := c.rand.Bytes(c.config.PaddedAccountSize - 8)
	copy(accountData[8:], randomBytes)

	err = c.put(address, accountData)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to put account: %w", err)
	}

	return accountID, address, nil
}

// Creates a new ERC20 contract and writes it to the database. Returns the address of the new ERC20 contract.
func (c *CryptoSim) createNewErc20Contract() ([]byte, error) {
	erc20ContractID := c.nextErc20ContractID
	c.nextErc20ContractID++

	erc20Address := c.rand.Address(contractPrefix, erc20ContractID)
	address := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, erc20Address)

	erc20Data := c.rand.Bytes(c.config.Erc20ContractSize)
	err := c.put(address, erc20Data)
	if err != nil {
		return nil, fmt.Errorf("failed to put ERC20 contract: %w", err)
	}

	return address, nil
}

// Commit the current batch if it has reached the configured number of transactions.
func (c *CryptoSim) maybeFinalizeBlock() error {
	if c.transactionsInCurrentBlock >= int64(c.config.TransactionsPerBlock) {
		return c.finalizeBlock()
	}
	return nil
}

// Push the current block out to the database.
func (c *CryptoSim) finalizeBlock() error {
	if c.transactionsInCurrentBlock == 0 {
		return nil
	}

	c.transactionsInCurrentBlock = 0

	changeSets := make([]*proto.NamedChangeSet, 0, c.transactionsInCurrentBlock+1)
	for _, cs := range c.batch.Iterator() {
		changeSets = append(changeSets, cs)
	}
	c.batch.Clear()

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

	// Periodically commit the changes to the database.
	c.uncommittedBlockCount++
	if c.uncommittedBlockCount >= int64(c.config.BlocksPerCommit) {
		_, err := c.db.Commit()
		if err != nil {
			return fmt.Errorf("failed to commit: %w", err)
		}
		c.uncommittedBlockCount = 0
	}

	return nil
}

// Insert a key-value pair into the database/cache.
//
// This method is safe to call concurrently with other calls to put() and get(). Is not thread
// safe with finalizeBlock().
func (c *CryptoSim) put(key []byte, value []byte) error {
	stringKey := string(key)

	pending, found := c.batch.Get(stringKey)
	if found {
		pending.Changeset.Pairs[0].Value = value
		return nil
	}

	c.batch.Put(stringKey, &proto.NamedChangeSet{
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
func (c *CryptoSim) get(key []byte) ([]byte, bool, error) {
	stringKey := string(key)

	pending, found := c.batch.Get(stringKey)
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

	fmt.Printf("Committing final batch.\n")

	if err := c.finalizeBlock(); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}
	if _, err := c.db.Commit(); err != nil {
		return fmt.Errorf("failed to commit database: %w", err)
	}

	fmt.Printf("Closing database.\n")
	err := c.db.Close()
	if err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	fmt.Printf("Benchmark terminated successfully.\n")

	// Specifically release rand, since it's likely to hold a lot of memory.
	c.rand = nil

	return nil
}
