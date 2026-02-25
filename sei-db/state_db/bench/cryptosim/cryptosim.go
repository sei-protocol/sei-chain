package cryptosim

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

const (
	accountPrefix    = 'a'
	contractPrefix   = 'c'
	ethStoragePrefix = 's'
)

// EVM key sizes (matches sei-db/common/evm).
const (
	AddressLen    = 20 // EVM address length
	SlotLen       = 32 // EVM storage slot length
	StorageKeyLen = AddressLen + SlotLen
)

// The test runner for the cryptosim benchmark.
type CryptoSim struct {
	ctx    context.Context
	cancel context.CancelFunc

	// The configuration for the benchmark.
	config *CryptoSimConfig

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

	// The data generator for the benchmark.
	dataGenerator *DataGenerator

	// The database for the benchmark.
	database *Database
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

	consoleUpdatePeriod := time.Duration(config.ConsoleUpdateIntervalSeconds * float64(time.Second))

	start := time.Now()

	ctx, cancel := context.WithCancel(ctx)

	database := NewDatabase(config, db)

	dataGenerator, err := NewDataGenerator(config, database, rand)
	if err != nil {
		cancel()
		db.Close()
		return nil, fmt.Errorf("failed to create data generator: %w", err)
	}

	c := &CryptoSim{
		ctx:                               ctx,
		cancel:                            cancel,
		config:                            config,
		consoleUpdatePeriod:               consoleUpdatePeriod,
		lastConsoleUpdateTime:             start,
		lastConsoleUpdateTransactionCount: 0,
		runHaltedChan:                     make(chan struct{}, 1),
		dataGenerator:                     dataGenerator,
		database:                          database,
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

// Prepopulate the database with the minimum number of accounts.
func (c *CryptoSim) setupAccounts() error {
	// Ensure that we at least have as many accounts as the hot set + 2. This simplifies logic elsewhere.
	if c.config.MinimumNumberOfAccounts < c.config.HotAccountSetSize+2 {
		c.config.MinimumNumberOfAccounts = c.config.HotAccountSetSize + 2
	}

	if c.dataGenerator.NextAccountID() >= int64(c.config.MinimumNumberOfAccounts) {
		return nil
	}

	fmt.Printf("Benchmark is configured to run with a minimum of %s accounts. Creating %s new accounts.\n",
		int64Commas(int64(c.config.MinimumNumberOfAccounts)),
		int64Commas(int64(c.config.MinimumNumberOfAccounts)-c.dataGenerator.NextAccountID()))

	for c.dataGenerator.NextAccountID() < int64(c.config.MinimumNumberOfAccounts) {
		if c.ctx.Err() != nil {
			fmt.Printf("benchmark aborted during account creation\n")
			break
		}

		_, _, err := c.dataGenerator.CreateNewAccount(c.config.PaddedAccountSize, true)
		if err != nil {
			return fmt.Errorf("failed to create new account: %w", err)
		}
		c.database.IncrementTransactionCount()
		err = c.database.MaybeFinalizeBlock(c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID())
		if err != nil {
			return fmt.Errorf("failed to maybe commit batch: %w", err)
		}

		if c.dataGenerator.NextAccountID()%c.config.SetupUpdateIntervalCount == 0 {
			fmt.Printf("Created %s of %s accounts.\r",
				int64Commas(c.dataGenerator.NextAccountID()), int64Commas(int64(c.config.MinimumNumberOfAccounts)))
		}
	}
	if c.dataGenerator.NextAccountID() >= c.config.SetupUpdateIntervalCount {
		fmt.Printf("\n")
	}
	fmt.Printf("Created %s of %s accounts.\n",
		int64Commas(c.dataGenerator.NextAccountID()), int64Commas(int64(c.config.MinimumNumberOfAccounts)))

	err := c.database.FinalizeBlock(c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID(), true)
	if err != nil {
		return fmt.Errorf("failed to finalize block: %w", err)
	}

	fmt.Printf("There are now %d accounts in the database.\n", c.dataGenerator.NextAccountID())

	return nil
}

// Prepopulate the database with the minimum number of ERC20 contracts.
func (c *CryptoSim) setupErc20Contracts() error {

	// Ensure that we at least have as many ERC20 contracts as the hot set + 1. This simplifies logic elsewhere.
	if c.config.MinimumNumberOfErc20Contracts < c.config.HotErc20ContractSetSize+1 {
		c.config.MinimumNumberOfErc20Contracts = c.config.HotErc20ContractSetSize + 1
	}

	if c.dataGenerator.NextErc20ContractID() >= int64(c.config.MinimumNumberOfErc20Contracts) {
		return nil
	}

	fmt.Printf("Benchmark is configured to run with a minimum of %s simulated ERC20 contracts. "+
		"Creating %s new ERC20 contracts.\n",
		int64Commas(int64(c.config.MinimumNumberOfErc20Contracts)),
		int64Commas(int64(c.config.MinimumNumberOfErc20Contracts)-c.dataGenerator.NextErc20ContractID()))

	for c.dataGenerator.NextErc20ContractID() < int64(c.config.MinimumNumberOfErc20Contracts) {
		if c.ctx.Err() != nil {
			fmt.Printf("benchmark aborted during ERC20 contract creation\n")
			break
		}

		c.database.IncrementTransactionCount()

		_, _, err := c.dataGenerator.CreateNewErc20Contract(c, c.config.Erc20ContractSize, true)
		if err != nil {
			return fmt.Errorf("failed to create new ERC20 contract: %w", err)
		}
		err = c.database.MaybeFinalizeBlock(c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID())
		if err != nil {
			return fmt.Errorf("failed to maybe commit batch: %w", err)
		}

		if c.dataGenerator.NextErc20ContractID()%c.config.SetupUpdateIntervalCount == 0 {
			fmt.Printf("Created %s of %s simulated ERC20 contracts.\r",
				int64Commas(c.dataGenerator.NextErc20ContractID()),
				int64Commas(int64(c.config.MinimumNumberOfErc20Contracts)))
		}
	}

	if c.dataGenerator.NextErc20ContractID() >= c.config.SetupUpdateIntervalCount {
		fmt.Printf("\n")
	}

	fmt.Printf("Created %s of %s simulated ERC20 contracts.\n",
		int64Commas(c.dataGenerator.NextErc20ContractID()), int64Commas(int64(c.config.MinimumNumberOfErc20Contracts)))

	err := c.database.FinalizeBlock(c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID(), true)
	if err != nil {
		return fmt.Errorf("failed to finalize block: %w", err)
	}

	fmt.Printf("There are now %d simulated ERC20 contracts in the database.\n", c.dataGenerator.NextErc20ContractID())

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
			if c.database.TransactionCount() > 0 {
				c.generateConsoleReport(true)
				fmt.Printf("\nTransaction workload halted.\n")
			}
			return
		default:

			txn, err := BuildTransaction(c.dataGenerator)
			if err != nil {
				fmt.Printf("\nfailed to build transaction: %v\n", err)
				os.Exit(1) // TODO use more elegant teardown mechanism
			}

			err = txn.Execute(c.database, c.dataGenerator.FeeCollectionAddress())
			if err != nil {
				fmt.Printf("\nfailed to execute transaction: %v\n", err)
				os.Exit(1)
			}

			err = c.database.MaybeFinalizeBlock(c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID())
			if err != nil {
				fmt.Printf("error finalizing block: %v\n", err)
				os.Exit(1)
			}

			c.database.IncrementTransactionCount()
			c.generateConsoleReport(false)
		}
	}
}

// Generates a human readable report of the benchmark's progress.
func (c *CryptoSim) generateConsoleReport(force bool) {

	// Future work: determine overhead of measuring time each cycle and change accordingly.

	now := time.Now()
	timeSinceLastUpdate := now.Sub(c.lastConsoleUpdateTime)
	transactionsSinceLastUpdate := c.database.TransactionCount() - c.lastConsoleUpdateTransactionCount

	if !force &&
		timeSinceLastUpdate < c.consoleUpdatePeriod &&
		transactionsSinceLastUpdate < int64(c.config.ConsoleUpdateIntervalTransactions) {

		// Not yet time to update the console.
		return
	}

	c.lastConsoleUpdateTime = now
	c.lastConsoleUpdateTransactionCount = c.database.TransactionCount()

	totalElapsedTime := now.Sub(c.startTimestamp)
	transactionsPerSecond := float64(c.database.TransactionCount()) / totalElapsedTime.Seconds()

	// Generate the report.
	fmt.Printf("%s txns executed in %v (%s txns/sec), total number of accounts: %s\r",
		int64Commas(c.database.TransactionCount()),
		totalElapsedTime,
		formatNumberFloat64(transactionsPerSecond, 2),
		int64Commas(c.dataGenerator.NextAccountID()))
}

// Shut down the benchmark and release any resources.
func (c *CryptoSim) Close() error {
	c.cancel()
	<-c.runHaltedChan

	err := c.database.Close(c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID())
	if err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	c.dataGenerator.Close()

	fmt.Printf("Benchmark terminated successfully.\n")

	return nil
}
